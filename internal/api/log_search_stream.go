package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

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

	return s.searchLogsViaStore(ctx, opts, emitChunk, emitStatus)
}

func (s *Server) searchLogsViaStore(ctx context.Context, opts logSearchOptions, emitChunk func([]string, int) error, emitStatus func(string, int) error) (logSearchSummary, error) {
	userConnIDs := make(map[string]map[string]struct{})
	if opts.query.hasUser {
		if emitStatus != nil {
			if err := emitStatus("Correlating user connections...", 0); err != nil {
				return logSearchSummary{}, err
			}
		}
		// The correlation pre-pass must see log lines that establish a
		// connection id ("[user] ... inbound connection") which can be far
		// older than the traffic lines that reference that id, so it can't
		// simply stop as soon as opts.limit matches are found the way the
		// real search pass below does. But without a bound it degenerates
		// into a full scan of the entire hot table (and any in-range cold
		// segments) on every bracketed-user search, no matter how small the
		// requested limit is. Cap it with a generous scan budget instead:
		// bounded work, while remaining large enough in practice not to miss
		// correlations for realistic limits and log volumes.
		budget := correlationScanBudget(opts.limit)
		scanned := 0
		if err := s.walkLogStore(ctx, opts, emitChunk, emitStatus, func(line string) error {
			scanned++
			normalized, ok := s.normalizeSearchLine(line, opts.timeRange, opts.censored)
			if ok {
				accumulateUserConnectionIDs(normalized, userConnIDs)
			}
			if scanned >= budget {
				return errStopLogWalk
			}
			return nil
		}); err != nil && !errors.Is(err, errStopLogWalk) {
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

	err := s.walkLogStore(ctx, opts, emitChunk, emitStatus, func(line string) error {
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

// errStopLogWalk is the api package's alias for core.ErrStopLogWalk. Using
// the exact same sentinel (rather than a locally-defined error with the same
// message) matters because core.LogStore.WalkHot recognizes a walk-stop
// signal via errors.Is(err, core.ErrStopLogWalk) - a distinct error value
// would not match, and while WalkHot happens to still terminate iteration in
// that case (any non-nil visitor error stops the row loop), it would return
// the "stop" as an unrecognized error instead of a clean nil, which is
// fragile for any caller that treats WalkHot's return value as meaningful.
var errStopLogWalk = core.ErrStopLogWalk

// correlationScanBudget bounds how many rows the hasUser correlation
// pre-pass in searchLogsViaStore will visit while building the
// connection-id-to-username map. Without a bound, a bracketed [user] search
// with no date range scans the entire hot log table (and any in-range cold
// segments) regardless of how small the requested match limit is, since the
// pre-pass's visitor never itself signals a stop. The budget scales with the
// requested limit so small searches stay cheap while still comfortably
// covering realistic log volumes.
func correlationScanBudget(limit int) int {
	const (
		multiplier = 200
		floor      = 20000
		ceiling    = 500000
	)
	budget := limit * multiplier
	if budget < floor {
		budget = floor
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

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

func (s *Server) walkLogStore(ctx context.Context, opts logSearchOptions, emitChunk func([]string, int) error, emitStatus func(string, int) error, visit func(string) error) error {
	if s.logStore == nil {
		return nil
	}

	tr := opts.timeRange
	fromMs, toMs := int64(0), int64(0)
	if tr.from != nil {
		fromMs = tr.from.UnixMilli()
	}
	if tr.to != nil {
		toMs = tr.to.UnixMilli()
	}

	// Determine if cold segments need scanning. An open-ended search has no
	// lower bound, so it must include the cold tier as well as hot rows.
	coldNeeded := fromMs == 0
	if !coldNeeded && fromMs > 0 {
		oldest, err := s.logStore.OldestHotTs(ctx)
		if err == nil && (oldest == 0 || oldest > fromMs) {
			coldNeeded = true
		}
	}

	visitRow := func(row core.LogRow) error {
		return visit(row.Raw)
	}

	// Hot tier first, then cold segments, all newest-first. This keeps mixed
	// hot+cold searches ordered by time and lets limit stop after newest matches.
	// FTS5 tokenizes by word boundaries, so a term like "git" would not match
	// "github" even though it's a substring. Always scan all rows and rely on
	// logLineMatchesQuery (strings.Contains) for correct substring matching.
	if err := s.logStore.WalkHot(ctx, "", fromMs, toMs, visitRow); err != nil {
		return err
	}

	if coldNeeded {
		coldDir := s.logColdDir()

		segs, err := s.logStore.SegmentsInRange(ctx, fromMs, toMs)
		if err != nil {
			return err
		}
		for _, seg := range segs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := walkColdSegmentReverse(ctx, filepath.Join(coldDir, seg.Filename), visit); err != nil {
				if errors.Is(err, errStopLogWalk) {
					return err
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Missing segment file = orphaned index row; log and continue.
				log.Printf("walkLogStore: cold segment %s read error: %v", seg.Filename, err)
			}
			// Flush whatever matched in this segment immediately so the client
			// sees results segment-by-segment without waiting for all cold
			// segments to finish.
			_ = emitStatus(fmt.Sprintf("searched %s", seg.Filename), -1)
		}
	}

	return nil
}

func (s *Server) logColdDir() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.LogColdDir) != "" {
		return strings.TrimSpace(s.config.LogColdDir)
	}
	if s != nil && s.config != nil && strings.TrimSpace(s.config.DatabasePath) != "" {
		return filepath.Join(filepath.Dir(s.config.DatabasePath), "logs")
	}
	return "data/logs"
}

// walkColdSegmentReverse opens a gzip segment, buffers all lines, then visits
// them in reverse order (newest-first within the segment).
// Checks ctx.Err() every 1000 lines during reverse iteration for cancellation.
func walkColdSegmentReverse(ctx context.Context, path string, visit func(string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	for i := len(lines) - 1; i >= 0; i-- {
		// Check cancellation every 1000 lines.
		if i%1000 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		if err := visit(lines[i]); err != nil {
			return err
		}
	}
	return nil
}

func collectSearchChunks(chunks [][]string) []string {
	collected := make([]string, 0)
	for i := len(chunks) - 1; i >= 0; i-- {
		chunk := chunks[i]
		for j := len(chunk) - 1; j >= 0; j-- {
			collected = append(collected, chunk[j])
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
