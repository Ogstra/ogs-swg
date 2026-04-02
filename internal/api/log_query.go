package api

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	logQueryAndSplitter = regexp.MustCompile(`(?i)\s+\bAND\b\s+`)
	logQueryOrSplitter  = regexp.MustCompile(`(?i)\s+\bOR\b\s+`)
	logConnectionIDRe   = regexp.MustCompile(`\[(\d+)(?:\s+[^\]]*)?\]`)
	logUserTermRe       = regexp.MustCompile(`^\[[^\[\]]+\]$`)
	logUserLineRe       = regexp.MustCompile(`:\s*(\[[^\[\]]+\])\s+inbound(?: packet)? connection\b`)
	logANSIEscapeRe     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

type logQueryTerm struct {
	raw      string
	needle   string
	userTerm string
}

type logQueryGroup struct {
	terms []logQueryTerm
}

type compiledLogQuery struct {
	raw        string
	groups     []logQueryGroup
	hasUser    bool
	hasOr      bool
	hasAnd     bool
	simpleText string
}

func (q compiledLogQuery) requiresPostFilter() bool {
	return q.hasUser || q.hasOr || q.hasAnd
}

func (q compiledLogQuery) isEmpty() bool {
	return len(q.groups) == 0
}

func compileLogQuery(raw string) compiledLogQuery {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return compiledLogQuery{}
	}

	orParts := logQueryOrSplitter.Split(trimmed, -1)
	query := compiledLogQuery{
		raw:    trimmed,
		groups: make([]logQueryGroup, 0, len(orParts)),
		hasOr:  len(orParts) > 1,
	}

	for _, orPart := range orParts {
		andParts := logQueryAndSplitter.Split(strings.TrimSpace(orPart), -1)
		if len(andParts) > 1 {
			query.hasAnd = true
		}

		group := logQueryGroup{terms: make([]logQueryTerm, 0, len(andParts))}
		for _, andPart := range andParts {
			term := compileLogQueryTerm(andPart)
			if term.raw == "" {
				continue
			}
			if term.userTerm != "" {
				query.hasUser = true
			}
			group.terms = append(group.terms, term)
		}

		if len(group.terms) > 0 {
			query.groups = append(query.groups, group)
		}
	}

	if len(query.groups) == 0 {
		fallback := compileLogQueryTerm(trimmed)
		if fallback.raw == "" {
			return compiledLogQuery{}
		}
		query.groups = []logQueryGroup{{terms: []logQueryTerm{fallback}}}
		query.hasUser = fallback.userTerm != ""
	}

	if !query.hasUser && !query.hasOr && !query.hasAnd && len(query.groups) == 1 && len(query.groups[0].terms) == 1 {
		query.simpleText = query.groups[0].terms[0].needle
	}

	return query
}

func compileLogQueryTerm(raw string) logQueryTerm {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return logQueryTerm{}
	}

	term := logQueryTerm{
		raw:    trimmed,
		needle: strings.ToLower(trimmed),
	}
	if logUserTermRe.MatchString(trimmed) {
		term.userTerm = strings.ToLower(trimmed)
	}
	return term
}

func sanitizeLogLine(line string) string {
	line = strings.TrimRight(line, "\r")
	return logANSIEscapeRe.ReplaceAllString(line, "")
}

func sanitizeLogLines(lines []string) {
	for i, line := range lines {
		lines[i] = sanitizeLogLine(line)
	}
}

func filterLogLines(lines []string, query compiledLogQuery) []string {
	if query.isEmpty() {
		return lines
	}

	userConnIDs := resolveUserConnectionIDsByToken(lines)
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if logLineMatchesQuery(line, query, userConnIDs) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func logLineMatchesQuery(line string, query compiledLogQuery, userConnIDs map[string]map[string]struct{}) bool {
	lineLower := strings.ToLower(line)
	connID := ""

	for _, group := range query.groups {
		groupMatches := true
		for _, term := range group.terms {
			if term.userTerm != "" {
				// A bracketed user query should always match the direct user-tagged line
				// itself, even if connection-id correlation fails for some runtime-specific
				// journal format variation.
				if strings.Contains(lineLower, term.userTerm) {
					continue
				}
				if connID == "" {
					connID = extractLogConnectionID(line)
				}
				if connID == "" {
					groupMatches = false
					break
				}
				idsForUser := userConnIDs[term.userTerm]
				if len(idsForUser) == 0 {
					groupMatches = false
					break
				}
				if _, ok := idsForUser[connID]; !ok {
					groupMatches = false
					break
				}
				continue
			}
			if !strings.Contains(lineLower, term.needle) {
				groupMatches = false
				break
			}
		}
		if groupMatches {
			return true
		}
	}

	return false
}

func resolveUserConnectionIDsByToken(lines []string) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, line := range lines {
		connID := extractLogConnectionID(line)
		if connID == "" {
			continue
		}

		match := logUserLineRe.FindStringSubmatch(line)
		if len(match) < 2 {
			continue
		}
		token := strings.ToLower(match[1])
		if !logUserTermRe.MatchString(token) {
			continue
		}
		if _, ok := result[token]; !ok {
			result[token] = make(map[string]struct{})
		}
		result[token][connID] = struct{}{}
	}
	return result
}

func extractLogConnectionID(line string) string {
	match := logConnectionIDRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func truncateRecentLogMatches(lines []string, limit int) ([]string, bool) {
	if limit <= 0 || len(lines) <= limit {
		return lines, false
	}
	return lines[len(lines)-limit:], true
}

func (s *Server) compileLogQuery(raw string) compiledLogQuery {
	return compileLogQuery(raw)
}

func (s *Server) readAllSearchableLogLines(ctx context.Context) ([]string, error) {
	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		return s.readAllJournalLogLines(ctx)
	}

	lines, err := readAllFileLines(s.config.AccessLogPath)
	if err == nil {
		return lines, nil
	}
	if s.config.LogSource == "file" {
		return s.readAllJournalLogLines(ctx)
	}
	return nil, err
}

func (s *Server) readAllJournalLogLines(ctx context.Context) ([]string, error) {
	if s.executor != nil {
		return s.executor.ReadAllJournal(ctx, "sing-box")
	}
	return readAllJournalLines("sing-box")
}

func readAllJournalLines(unit string) ([]string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		log.Printf("journalctl not found: %v", err)
		return []string{"(journalctl not available on this system)"}, nil
	}

	cmd := exec.Command("journalctl", "-u", unit, "--no-pager", "--merge", "-o", "cat")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" || strings.Contains(strings.ToLower(msg), "no entries") || len(out) == 0 {
			return []string{}, nil
		}
		return nil, err
	}

	data := strings.TrimSpace(string(out))
	if data == "" {
		return []string{}, nil
	}
	return strings.Split(data, "\n"), nil
}

func readAllFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
