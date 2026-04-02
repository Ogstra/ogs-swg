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
	logConnectionIDRe   = regexp.MustCompile(`\[(\d+)(?:\s+[^\]]*)?\]`)
)

type logQueryTerm struct {
	raw      string
	needle   string
	userName string
}

type compiledLogQuery struct {
	terms     []logQueryTerm
	textTerms []logQueryTerm
	userTerms []logQueryTerm
}

func (q compiledLogQuery) requiresPostFilter() bool {
	return len(q.userTerms) > 0 || len(q.terms) > 1
}

func compileLogQuery(raw string, knownUsers map[string]string) compiledLogQuery {
	parts := splitLogQueryTerms(raw)
	query := compiledLogQuery{
		terms: make([]logQueryTerm, 0, len(parts)),
	}

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		term := logQueryTerm{
			raw:    trimmed,
			needle: strings.ToLower(trimmed),
		}
		if knownUsers != nil {
			if userName, ok := knownUsers[normalizeLogUserLookup(trimmed)]; ok {
				term.userName = userName
			}
		}

		query.terms = append(query.terms, term)
		if term.userName != "" {
			query.userTerms = append(query.userTerms, term)
			continue
		}
		query.textTerms = append(query.textTerms, term)
	}

	return query
}

func splitLogQueryTerms(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	parts := logQueryAndSplitter.Split(trimmed, -1)
	if len(parts) == 0 {
		return []string{trimmed}
	}
	return parts
}

func normalizeLogUserLookup(term string) string {
	trimmed := strings.TrimSpace(term)
	trimmed = strings.Trim(trimmed, `"'`)
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && len(trimmed) >= 2 {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	return strings.ToLower(strings.TrimSpace(trimmed))
}

func filterLogLines(lines []string, query compiledLogQuery) []string {
	if len(query.terms) == 0 {
		return lines
	}

	var matchingConnectionIDs map[string]struct{}
	if len(query.userTerms) > 0 {
		matchingConnectionIDs = resolveUserConnectionIDs(lines, query.userTerms)
		if len(matchingConnectionIDs) == 0 {
			return nil
		}
	}

	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		lineLower := strings.ToLower(line)
		if len(query.userTerms) > 0 {
			connID := extractLogConnectionID(line)
			if connID == "" {
				continue
			}
			if _, ok := matchingConnectionIDs[connID]; !ok {
				continue
			}
		}

		matchesTextTerms := true
		for _, term := range query.textTerms {
			if !strings.Contains(lineLower, term.needle) {
				matchesTextTerms = false
				break
			}
		}
		if matchesTextTerms {
			filtered = append(filtered, line)
		}
	}

	return filtered
}

func resolveUserConnectionIDs(lines []string, userTerms []logQueryTerm) map[string]struct{} {
	if len(userTerms) == 0 {
		return nil
	}

	uniqueUsers := make(map[string]struct{}, len(userTerms))
	for _, term := range userTerms {
		userLower := strings.ToLower(strings.TrimSpace(term.userName))
		if userLower != "" {
			uniqueUsers[userLower] = struct{}{}
		}
	}

	var sets []map[string]struct{}
	for userLower := range uniqueUsers {
		userToken := "[" + userLower + "]"
		userConnIDs := make(map[string]struct{})
		for _, line := range lines {
			if !strings.Contains(strings.ToLower(line), userToken) {
				continue
			}
			connID := extractLogConnectionID(line)
			if connID == "" {
				continue
			}
			userConnIDs[connID] = struct{}{}
		}
		sets = append(sets, userConnIDs)
	}

	if len(sets) == 0 {
		return nil
	}

	result := sets[0]
	for _, set := range sets[1:] {
		for connID := range result {
			if _, ok := set[connID]; !ok {
				delete(result, connID)
			}
		}
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
	return compileLogQuery(raw, s.knownLogUsers())
}

func (s *Server) knownLogUsers() map[string]string {
	if s == nil || s.config == nil {
		return nil
	}

	users, err := s.config.GetActiveUsers()
	if err != nil {
		log.Printf("log query: cannot load sing-box users: %v", err)
		return nil
	}

	knownUsers := make(map[string]string, len(users))
	for _, user := range users {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			continue
		}
		knownUsers[strings.ToLower(name)] = name
	}
	return knownUsers
}

func (s *Server) readAllSearchableLogLines(ctx context.Context) ([]string, error) {
	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		if s.executor != nil {
			return s.executor.ReadAllJournal(ctx, "sing-box")
		}
		return readAllJournalLines("sing-box")
	}

	lines, err := readAllFileLines(s.config.AccessLogPath)
	if err == nil {
		return lines, nil
	}
	if s.config.LogSource == "file" {
		if s.executor != nil {
			return s.executor.ReadAllJournal(ctx, "sing-box")
		}
		return readAllJournalLines("sing-box")
	}
	return nil, err
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
