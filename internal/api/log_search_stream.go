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
		if err := s.walkLogStore(ctx, opts, emitChunk, emitStatus, func(line string) error {
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

	// Determine if cold segments need scanning: query reaches before oldest hot row.
	coldNeeded := false
	if fromMs > 0 {
		oldest, err := s.logStore.OldestHotTs(ctx)
		if err == nil && (oldest == 0 || oldest > fromMs) {
			coldNeeded = true
		}
	}
	// fromMs == 0 means no lower time bound — hot only (D-05).

	visitRow := func(row core.LogRow) error {
		return visit(row.Raw)
	}

	// Cold first (oldest range), newest-first within: iterate segments newest→oldest,
	// and within each segment scan lines in reverse (matches previous file behavior).
	if coldNeeded {
		coldDir := filepath.Join(filepath.Dir(s.config.DatabasePath), "logs")

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

	// Hot tier: WalkHot in newest-first order.
	simpleText := ""
	if !opts.query.requiresPostFilter() {
		simpleText = opts.query.simpleText
	}
	return s.logStore.WalkHot(ctx, simpleText, fromMs, toMs, visitRow)
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
