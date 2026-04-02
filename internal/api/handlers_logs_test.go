package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// logsExecutorStub overrides ReadJournal and SearchJournal to return controlled lines.
type logsExecutorStub struct {
	singboxConfigExecutorStub
	journalLines []string
}

func (s *logsExecutorStub) ReadJournal(_ context.Context, _ string, limit int) ([]string, error) {
	if limit <= 0 || limit >= len(s.journalLines) {
		out := make([]string, len(s.journalLines))
		copy(out, s.journalLines)
		return out, nil
	}
	out := make([]string, limit)
	copy(out, s.journalLines[len(s.journalLines)-limit:])
	return out, nil
}

func (s *logsExecutorStub) ReadAllJournal(_ context.Context, _ string) ([]string, error) {
	out := make([]string, len(s.journalLines))
	copy(out, s.journalLines)
	return out, nil
}

func (s *logsExecutorStub) SearchJournal(_ context.Context, _ string, query string, _ int) ([]string, error) {
	// Return all lines that contain the query (case-insensitive not required by stub —
	// just do a simple substring match so tests can reason about it).
	var result []string
	q := query
	for _, ln := range s.journalLines {
		if containsInsensitive(ln, q) {
			result = append(result, ln)
		}
	}
	return result, nil
}

func containsInsensitive(s, substr string) bool {
	sl := make([]byte, len(s))
	copy(sl, s)
	subl := make([]byte, len(substr))
	copy(subl, substr)
	for i := range sl {
		if sl[i] >= 'A' && sl[i] <= 'Z' {
			sl[i] += 32
		}
	}
	for i := range subl {
		if subl[i] >= 'A' && subl[i] <= 'Z' {
			subl[i] += 32
		}
	}
	return len(subl) == 0 || contains(string(sl), string(subl))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexStr(s, substr) >= 0)
}

func indexStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func newLogsTestServer(t *testing.T, lines []string) (*Server, *logsExecutorStub) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	stub := &logsExecutorStub{
		singboxConfigExecutorStub: singboxConfigExecutorStub{data: []byte(`{
			"inbounds": [
				{
					"type": "vless",
					"tag": "in-reality",
					"users": [
						{"name":"ALPHA","uuid":"11111111-1111-1111-1111-111111111111"},
						{"name":"BETA","uuid":"22222222-2222-2222-2222-222222222222"}
					]
				}
			]
		}`)},
		journalLines: lines,
	}
	cfg := &core.Config{
		EnableSingbox:     true,
		LogSource:         "journal",
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   []string{"in-reality"},
		StatsInbounds:     []string{"in-reality"},
	}
	cfg.SetExecutor(stub)
	return NewServer(store, cfg, stub), stub
}

func newLogsTestServerWithFileSource(t *testing.T, journalLines, fileLines []string) (*Server, *logsExecutorStub) {
	t.Helper()
	srv, stub := newLogsTestServer(t, journalLines)
	accessLogPath := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(accessLogPath, []byte(joinLines(fileLines)), 0644); err != nil {
		t.Fatalf("WriteFile(access.log): %v", err)
	}
	srv.config.LogSource = "file"
	srv.config.AccessLogPath = accessLogPath
	return srv, stub
}

func requestWithPerms(r *http.Request, perms *core.PanelUserPermissions) *http.Request {
	ctx := context.WithValue(r.Context(), permissionsContextKey, perms)
	return r.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// TestEnsureGrantablePermissions — can_read_logs_censored privilege escalation guard
// ---------------------------------------------------------------------------

func TestEnsureGrantablePermissions_CanReadLogsCensored(t *testing.T) {
	tests := []struct {
		name        string
		caller      core.PanelUserPermissions
		requested   core.PanelUserPermissions
		wantErr     bool
		errContains string
	}{
		{
			name:      "caller with logs-only (no censored) can grant censored",
			caller:    core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: false},
			requested: core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: true},
			wantErr:   false,
		},
		{
			name:      "caller with censored can grant censored",
			caller:    core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: true},
			requested: core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: true},
			wantErr:   false,
		},
		{
			// Normalize implies CanReadLogs when CanReadLogsCensored is true, so the
			// can_read_logs check fires before can_read_logs_censored — either way the
			// caller without any log access is blocked.
			name:        "caller without any log access cannot grant censored",
			caller:      core.PanelUserPermissions{CanReadLogs: false, CanReadLogsCensored: false},
			requested:   core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: true},
			wantErr:     true,
			errContains: "cannot grant can_read_logs",
		},
		{
			name:      "granting false is always allowed",
			caller:    core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: false},
			requested: core.PanelUserPermissions{CanReadLogs: true, CanReadLogsCensored: false},
			wantErr:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caller := &tc.caller
			requested := tc.requested
			err := ensureGrantablePermissions(caller, requested)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !containsInsensitive(err.Error(), tc.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleGetLogs
// ---------------------------------------------------------------------------

const rawLogLine = "2024/01/01 12:00:00 accepted from 1.2.3.4:5678 to example.com:443"
const censoredLogLine = "2024/01/01 12:00:00 accepted from ***:*** to ***:***"
const userTaggedLogLine = "2024/01/01 12:00:01 INFO [3814834843 76ms] inbound/vless[in-reality]: [ALPHA] inbound connection to example.com"
const userOutboundLogLine = "2024/01/01 12:00:02 INFO [3814834843 76ms] outbound/direct[direct]: outbound connection to example.com:443"
const otherUserTaggedLogLine = "2024/01/01 12:00:03 INFO [99887766 18ms] inbound/vless[in-reality]: [BETA] inbound connection to example.net"
const textAndLogLine = "2024/01/01 12:00:04 ERROR timeout while dialing upstream"
const hysteriaUserTaggedLogLine = "-0300 2026-04-01 23:05:23 INFO [2254766407 0ms] inbound/hysteria2[hysteria2]: [ALPHA-H2] inbound connection to service.example:443"
const hysteriaOutboundLogLine = "-0300 2026-04-01 23:05:23 INFO [2254766407 1ms] outbound/direct[direct]: outbound connection to service.example:443"
const ansiUserTaggedLogLineRaw = "-0300 2026-04-01 23:24:36 \x1b[36mINFO\x1b[0m [\x1b[38;5;155m625305227\x1b[0m 35ms] inbound/vless[in-reality]: [ALPHA] inbound connection to service.example:5228"
const ansiUserTaggedLogLineSanitized = "-0300 2026-04-01 23:24:36 INFO [625305227 35ms] inbound/vless[in-reality]: [ALPHA] inbound connection to service.example:5228"
const ansiUserOutboundLogLineRaw = "-0300 2026-04-01 23:24:36 \x1b[36mINFO\x1b[0m [\x1b[38;5;155m625305227\x1b[0m 35ms] outbound/direct[direct]: outbound connection to service.example:5228"
const ansiUserOutboundLogLineSanitized = "-0300 2026-04-01 23:24:36 INFO [625305227 35ms] outbound/direct[direct]: outbound connection to service.example:5228"
const packetUserTaggedLogLine = "-0300 2026-04-01 07:29:29 INFO [3007084205 118ms] inbound/vless[in-reality]: [ALPHA] inbound packet connection to 1.1.1.1:53"
const packetDNSLogLine = "-0300 2026-04-01 07:29:29 INFO [3007084205 118ms] dns: cached A resolver.example. 48827 IN A 8.8.8.8"
const otherPacketUserTaggedLogLine = "-0300 2026-04-01 07:29:29 INFO [98312978 118ms] inbound/vless[in-reality]: [ALPHA] inbound packet connection to 1.1.1.1:53"

func TestHandleGetLogs(t *testing.T) {
	tests := []struct {
		name          string
		perms         *core.PanelUserPermissions
		query         string // ?user= filter
		wantInBody    string
		wantNotInBody string
		wantEmpty     bool // expect no real log lines (only the "no log lines found" sentinel)
	}{
		{
			name: "censored user sees redacted line",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: true,
			},
			wantInBody:    "***:***",
			wantNotInBody: "1.2.3.4",
		},
		{
			name: "full-permission user sees original line",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: false,
			},
			wantInBody: "1.2.3.4",
		},
		{
			name: "censored user filter on raw IP returns empty",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: true,
			},
			query:     "1.2.3.4",
			wantEmpty: true,
		},
		{
			name: "censored user filter on non-IP term (accepted) returns censored line",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: true,
			},
			query:         "accepted",
			wantInBody:    "***:***",
			wantNotInBody: "1.2.3.4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newLogsTestServer(t, []string{rawLogLine})

			url := "/api/logs"
			if tc.query != "" {
				url += "?user=" + tc.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			req = requestWithPerms(req, tc.perms)
			rr := httptest.NewRecorder()

			srv.handleGetLogs(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			var resp map[string]interface{}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			logsRaw, ok := resp["logs"].([]interface{})
			if !ok {
				t.Fatalf("logs field missing or wrong type: %#v", resp["logs"])
			}

			logs := make([]string, len(logsRaw))
			for i, v := range logsRaw {
				logs[i], _ = v.(string)
			}

			body := joinLines(logs)

			if tc.wantEmpty {
				// All lines should be sentinel "(no log lines found...)" — none should be rawLogLine
				if containsInsensitive(body, "1.2.3.4") || containsInsensitive(body, "***:***") {
					t.Fatalf("expected empty results but got: %v", logs)
				}
				return
			}

			if tc.wantInBody != "" && !containsInsensitive(body, tc.wantInBody) {
				t.Fatalf("expected %q in response body, got: %v", tc.wantInBody, logs)
			}
			if tc.wantNotInBody != "" && containsInsensitive(body, tc.wantNotInBody) {
				t.Fatalf("expected %q NOT in response body, but it was present: %v", tc.wantNotInBody, logs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHandleSearchLogs
// ---------------------------------------------------------------------------

func TestHandleSearchLogs(t *testing.T) {
	tests := []struct {
		name          string
		perms         *core.PanelUserPermissions
		query         string
		wantInBody    string
		wantNotInBody string
		wantEmpty     bool
	}{
		{
			name: "censored user search on non-IP term sees censored lines",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: true,
			},
			query:         "accepted",
			wantInBody:    "***:***",
			wantNotInBody: "1.2.3.4",
		},
		{
			name: "censored user search on raw IP returns empty",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: true,
			},
			query:     "1.2.3.4",
			wantEmpty: true,
		},
		{
			name: "full-permission user search on raw IP returns unredacted line",
			perms: &core.PanelUserPermissions{
				CanReadLogs:         true,
				CanReadLogsCensored: false,
			},
			query:      "1.2.3.4",
			wantInBody: "1.2.3.4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newLogsTestServer(t, []string{rawLogLine})

			req := httptest.NewRequest(http.MethodGet, "/api/logs/search?q="+url.QueryEscape(tc.query), nil)
			req = requestWithPerms(req, tc.perms)
			rr := httptest.NewRecorder()

			srv.handleSearchLogs(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			var resp map[string]interface{}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			logsRaw, ok := resp["logs"].([]interface{})
			if !ok {
				t.Fatalf("logs field missing or wrong type: %#v", resp["logs"])
			}

			logs := make([]string, len(logsRaw))
			for i, v := range logsRaw {
				logs[i], _ = v.(string)
			}

			body := joinLines(logs)

			if tc.wantEmpty {
				if containsInsensitive(body, "1.2.3.4") || containsInsensitive(body, "***:***") {
					t.Fatalf("expected empty results but got: %v", logs)
				}
				return
			}

			if tc.wantInBody != "" && !containsInsensitive(body, tc.wantInBody) {
				t.Fatalf("expected %q in response body, got: %v", tc.wantInBody, logs)
			}
			if tc.wantNotInBody != "" && containsInsensitive(body, tc.wantNotInBody) {
				t.Fatalf("expected %q NOT in response body, but it was present: %v", tc.wantNotInBody, logs)
			}
		})
	}
}

func TestHandleGetLogs_UserQueryFollowsConnectionID(t *testing.T) {
	srv, _ := newLogsTestServer(t, []string{
		userTaggedLogLine,
		userOutboundLogLine,
		otherUserTaggedLogLine,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=%5BALPHA%5D", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, userTaggedLogLine) {
		t.Fatalf("expected tagged user line in response, got: %v", resp.Logs)
	}
	if !containsInsensitive(body, userOutboundLogLine) {
		t.Fatalf("expected correlated outbound line in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, otherUserTaggedLogLine) {
		t.Fatalf("did not expect other user's line in response, got: %v", resp.Logs)
	}
}

func TestHandleGetLogs_PlainTermDoesNotTriggerUserCorrelation(t *testing.T) {
	srv, _ := newLogsTestServer(t, []string{
		userTaggedLogLine,
		userOutboundLogLine,
		otherUserTaggedLogLine,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=ALPHA", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, userTaggedLogLine) {
		t.Fatalf("expected tagged user line in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, userOutboundLogLine) {
		t.Fatalf("plain text query must not pull correlated outbound lines, got: %v", resp.Logs)
	}
}

func TestHandleGetLogs_UserQueryUsesFullHistoryNotJustTailWindow(t *testing.T) {
	lines := []string{userTaggedLogLine}
	for i := 0; i < 20; i++ {
		lines = append(lines, textAndLogLine)
	}
	lines = append(lines, userOutboundLogLine, otherUserTaggedLogLine)

	srv, _ := newLogsTestServer(t, lines)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=%5BALPHA%5D&limit=20", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, userOutboundLogLine) {
		t.Fatalf("expected correlated outbound line in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, otherUserTaggedLogLine) {
		t.Fatalf("did not expect other user's line in response, got: %v", resp.Logs)
	}
}

func TestHandleGetLogs_UserQueryMatchesDirectTaggedLineWithoutCorrelation(t *testing.T) {
	lines := []string{
		"2024/01/01 12:00:01 INFO inbound/vless[in-reality]: [ALPHA] inbound connection to example.com",
		textAndLogLine,
	}

	srv, _ := newLogsTestServer(t, lines)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=%5BALPHA%5D", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, "[ALPHA] inbound connection") {
		t.Fatalf("expected direct tagged line in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, textAndLogLine) {
		t.Fatalf("did not expect unrelated line in response, got: %v", resp.Logs)
	}
}

func TestHandleGetLogs_UserQueryStripsANSIAndFollowsConnectionID(t *testing.T) {
	srv, _ := newLogsTestServer(t, []string{
		ansiUserTaggedLogLineRaw,
		ansiUserOutboundLogLineRaw,
		otherUserTaggedLogLine,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=%5BALPHA%5D", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, ansiUserTaggedLogLineSanitized) {
		t.Fatalf("expected sanitized tagged user line in response, got: %v", resp.Logs)
	}
	if !containsInsensitive(body, ansiUserOutboundLogLineSanitized) {
		t.Fatalf("expected sanitized correlated outbound line in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, "\x1b[") {
		t.Fatalf("expected ANSI sequences to be stripped, got: %q", body)
	}
}

func TestHandleGetLogs_UserPacketQueryFollowsConnectionIDToDNSLines(t *testing.T) {
	srv, _ := newLogsTestServer(t, []string{
		otherPacketUserTaggedLogLine,
		packetUserTaggedLogLine,
		packetDNSLogLine,
		otherUserTaggedLogLine,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/logs?q=%5BALPHA%5D", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleGetLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, packetUserTaggedLogLine) {
		t.Fatalf("expected packet user line in response, got: %v", resp.Logs)
	}
	if !containsInsensitive(body, packetDNSLogLine) {
		t.Fatalf("expected dns line correlated by connection id in response, got: %v", resp.Logs)
	}
	if containsInsensitive(body, otherUserTaggedLogLine) {
		t.Fatalf("did not expect unrelated user line in response, got: %v", resp.Logs)
	}
}

func TestHandleSearchLogs_UserQuerySupportsBracketedUserAndAndOperator(t *testing.T) {
	srv, _ := newLogsTestServer(t, []string{
		userTaggedLogLine,
		userOutboundLogLine,
		otherUserTaggedLogLine,
		textAndLogLine,
	})

	tests := []struct {
		name       string
		query      string
		wantInBody string
		wantNotIn  string
	}{
		{
			name:       "bracketed user query follows connection id",
			query:      "[ALPHA]",
			wantInBody: userOutboundLogLine,
			wantNotIn:  otherUserTaggedLogLine,
		},
		{
			name:       "user and term narrows to correlated line",
			query:      "[ALPHA] AND outbound/direct",
			wantInBody: userOutboundLogLine,
			wantNotIn:  userTaggedLogLine,
		},
		{
			name:       "or returns either text or user-correlated matches",
			query:      "[BETA] OR timeout",
			wantInBody: otherUserTaggedLogLine,
			wantNotIn:  userOutboundLogLine,
		},
		{
			name:       "plain text and works without user detection",
			query:      "error AND timeout",
			wantInBody: textAndLogLine,
			wantNotIn:  userTaggedLogLine,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/logs/search?q="+url.QueryEscape(tc.query), nil)
			req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
			rr := httptest.NewRecorder()

			srv.handleSearchLogs(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			var resp struct {
				Logs []string `json:"logs"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			body := joinLines(resp.Logs)
			if !containsInsensitive(body, tc.wantInBody) {
				t.Fatalf("expected %q in response body, got: %v", tc.wantInBody, resp.Logs)
			}
			if tc.wantNotIn != "" && containsInsensitive(body, tc.wantNotIn) {
				t.Fatalf("expected %q not to appear in response body, got: %v", tc.wantNotIn, resp.Logs)
			}
		})
	}
}

func TestHandleSearchLogs_FileModeFallsBackToJournalForBracketQuery(t *testing.T) {
	srv, _ := newLogsTestServerWithFileSource(t,
		[]string{hysteriaUserTaggedLogLine, hysteriaOutboundLogLine},
		[]string{textAndLogLine},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/search?q=%5BALPHA-H2%5D", nil)
	req = requestWithPerms(req, &core.PanelUserPermissions{CanReadLogs: true})
	rr := httptest.NewRecorder()

	srv.handleSearchLogs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Logs []string `json:"logs"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	body := joinLines(resp.Logs)
	if !containsInsensitive(body, hysteriaUserTaggedLogLine) {
		t.Fatalf("expected journal fallback to include hysteria user line, got: %v", resp.Logs)
	}
	if !containsInsensitive(body, hysteriaOutboundLogLine) {
		t.Fatalf("expected journal fallback to include correlated outbound line, got: %v", resp.Logs)
	}
}

func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}
