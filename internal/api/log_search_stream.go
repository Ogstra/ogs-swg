package api

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type journalWalker interface {
	WalkJournal(ctx context.Context, unit string, newestFirst bool, visit func(string) error) error
}

type logSearchOptions struct {
	query     compiledLogQuery
	timeRange logTimeRange
	censored  bool
	limit     int
	chunkSize int
}

type logSearchSummary struct {
	matched   int
	truncated bool
}

func (s *Server) searchLogsIncrementally(ctx context.Context, opts logSearchOptions, emitChunk func([]string, int) error, emitStatus func(string, int) error) (logSearchSummary, error) {
	if opts.limit <= 0 {
		opts.limit = 200
	}
	if opts.chunkSize <= 0 {
		opts.chunkSize = 100
	}

	// Acquire the log-search semaphore before doing any I/O or CPU work.
	// This caps concurrent full-log scans so that rapid or parallel requests
	// cannot saturate all CPU cores. The semaphore is released as soon as
	// the scan completes (or the caller's context is cancelled).
	if s.logSearchSem != nil {
		select {
		case s.logSearchSem <- struct{}{}:
			defer func() { <-s.logSearchSem }()
		case <-ctx.Done():
			return logSearchSummary{}, ctx.Err()
		}
	}

	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		return s.searchLogsOnSource(ctx, logSearchSourceJournal, opts, emitChunk, emitStatus)
	}

	summary, err := s.searchLogsOnSource(ctx, logSearchSourceFile, opts, emitChunk, emitStatus)
	if err == nil && summary.matched > 0 {
		return summary, nil
	}
	if s.config.LogSource != "file" {
		return summary, err
	}

	journalSummary, journalErr := s.searchLogsOnSource(ctx, logSearchSourceJournal, opts, emitChunk, emitStatus)
	if journalErr == nil {
		return journalSummary, nil
	}
	if err != nil {
		return summary, err
	}
	return journalSummary, journalErr
}

type logSearchSource string

const (
	logSearchSourceJournal logSearchSource = "journal"
	logSearchSourceFile    logSearchSource = "file"
)

func (s *Server) searchLogsOnSource(ctx context.Context, source logSearchSource, opts logSearchOptions, emitChunk func([]string, int) error, emitStatus func(string, int) error) (logSearchSummary, error) {
	userConnIDs := make(map[string]map[string]struct{})
	if opts.query.hasUser {
		if emitStatus != nil {
			if err := emitStatus("Correlating user connections...", 0); err != nil {
				return logSearchSummary{}, err
			}
		}
		if err := s.walkSearchSource(ctx, source, opts, func(line string) error {
			normalized, ok := s.normalizeSearchLine(line, opts.timeRange, opts.censored)
			if !ok {
				return nil
			}
			accumulateUserConnectionIDs(normalized, userConnIDs)
			return nil
		}); err != nil {
			return logSearchSummary{}, err
		}
	}

	if emitStatus != nil {
		if err := emitStatus("Searching logs...", 0); err != nil {
			return logSearchSummary{}, err
		}
	}

	var (
		matched   int
		truncated bool
		pending   []string
	)

	flush := func() error {
		if len(pending) == 0 || emitChunk == nil {
			return nil
		}
		chunk := make([]string, len(pending))
		copy(chunk, pending)
		pending = pending[:0]
		return emitChunk(chunk, matched)
	}

	err := s.walkSearchSource(ctx, source, opts, func(line string) error {
		normalized, ok := s.normalizeSearchLine(line, opts.timeRange, opts.censored)
		if !ok {
			return nil
		}
		if !logLineMatchesQuery(normalized, opts.query, userConnIDs) {
			return nil
		}
		if matched >= opts.limit {
			truncated = true
			return errStopLogWalk
		}

		matched++
		pending = append(pending, normalized)
		if len(pending) >= opts.chunkSize {
			return flush()
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopLogWalk) {
		return logSearchSummary{}, err
	}
	if err := flush(); err != nil {
		return logSearchSummary{}, err
	}
	return logSearchSummary{matched: matched, truncated: truncated}, nil
}

var errStopLogWalk = errors.New("stop log walk")

func (s *Server) normalizeSearchLine(line string, tr logTimeRange, censored bool) (string, bool) {
	line = sanitizeLogLine(line)
	if tr.from != nil || tr.to != nil {
		ts, ok := extractLogTimestamp(line, s.now)
		if !ok {
			return "", false
		}
		if tr.from != nil && ts.Before(*tr.from) {
			return "", false
		}
		if tr.to != nil && ts.After(*tr.to) {
			return "", false
		}
	}
	if censored {
		line = core.CensorLine(line)
	}
	return line, true
}

func accumulateUserConnectionIDs(line string, result map[string]map[string]struct{}) {
	connID := extractLogConnectionID(line)
	if connID == "" {
		return
	}

	match := logUserLineRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return
	}
	token := strings.ToLower(match[1])
	if !logUserTermRe.MatchString(token) {
		return
	}
	if _, ok := result[token]; !ok {
		result[token] = make(map[string]struct{})
	}
	result[token][connID] = struct{}{}
}

func (s *Server) walkSearchSource(ctx context.Context, source logSearchSource, opts logSearchOptions, visit func(string) error) error {
	switch source {
	case logSearchSourceFile:
		return walkFileLinesReverse(s.config.AccessLogPath, visit)
	case logSearchSourceJournal:
		return s.walkJournalLogLines(ctx, opts, visit)
	default:
		return nil
	}
}

func (s *Server) walkJournalLogLines(ctx context.Context, opts logSearchOptions, visit func(string) error) error {
	// Use the simple text as a journalctl --grep hint when the query doesn't
	// require Go-side post-filtering (no user correlation, no AND/OR logic).
	// This lets journalctl filter at the journal reader level — far cheaper
	// than streaming every line to the Go process.
	grepFilter := ""
	if !opts.query.requiresPostFilter() {
		grepFilter = opts.query.simpleText
	}
	if walker, ok := s.executor.(journalWalker); ok {
		return walker.WalkJournal(ctx, "sing-box", true, visit)
	}
	return walkJournalLinesReverse(ctx, "sing-box", grepFilter, visit)
}

func walkJournalLinesReverse(ctx context.Context, unit string, grepFilter string, visit func(string) error) error {
	if _, err := exec.LookPath("journalctl"); err != nil {
		return nil
	}

	unitFull := unit
	if !strings.Contains(unitFull, ".") {
		unitFull += ".service"
	}

	args := []string{"-u", unitFull, "--no-pager", "--merge", "-o", "cat", "--reverse"}
	if grepFilter != "" {
		args = append(args, "--grep", grepFilter)
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		var msg strings.Builder
		for scanner.Scan() {
			if msg.Len() > 0 {
				msg.WriteString("\n")
			}
			msg.WriteString(scanner.Text())
		}
		if scanErr := scanner.Err(); scanErr != nil {
			errCh <- scanErr
			return
		}
		if msg.Len() > 0 {
			errCh <- errors.New(msg.String())
			return
		}
		errCh <- nil
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if ctx != nil && ctx.Err() != nil {
			_ = cmd.Wait()
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := visit(line); err != nil {
			_ = cmd.Wait()
			if errors.Is(err, errStopLogWalk) {
				return err
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return err
	}
	waitErr := cmd.Wait()
	stderrErr := <-errCh
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		if stderrErr != nil {
			return stderrErr
		}
		return waitErr
	}
	return nil
}

func walkFileLinesReverse(path string, visit func(string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const chunkSize = 64 * 1024
	info, err := f.Stat()
	if err != nil {
		return err
	}

	// Allocate once and reuse for every chunk read. This avoids a make([]byte)
	// allocation per 64 KB chunk when walking multi-MB log files.
	buf := make([]byte, chunkSize)
	var rem string
	for offset := info.Size(); offset > 0; {
		readSize := int64(chunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		chunk := buf[:readSize]
		if _, err := f.ReadAt(chunk, offset); err != nil {
			return err
		}
		data := string(chunk) + rem
		lines := strings.Split(data, "\n")
		if offset > 0 && len(lines) > 0 {
			rem = lines[0]
			lines = lines[1:]
		} else {
			rem = ""
		}
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			if err := visit(line); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(rem) != "" {
		if err := visit(strings.TrimSpace(rem)); err != nil {
			return err
		}
	}
	return nil
}

func collectSearchChunks(chunks [][]string) []string {
	collected := make([]string, 0)
	for _, chunk := range chunks {
		for i := len(chunk) - 1; i >= 0; i-- {
			collected = append(collected, chunk[i])
		}
	}
	return collected
}

func parseSearchPageParams(r *http.Request) (page, pageSize, effectiveLimit int) {
	pageSize = 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500000 {
			pageSize = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 500000 {
			pageSize = v
		}
	}
	page = 1
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 && v <= 1000 {
			page = v
		}
	}
	effectiveLimit = page * pageSize
	if effectiveLimit > 500000 {
		effectiveLimit = 500000
	}
	return page, pageSize, effectiveLimit
}
