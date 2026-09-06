package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
)

// updateDashGolden rewrites internal/api/testdata/dashboard_golden.json from
// the current (unmodified) behavior of handleGetDashboardData. Run with:
//
//	go test ./internal/api -run TestDashboardPayloadGolden -updatedashgolden -count=1
var updateDashGolden = flag.Bool("updatedashgolden", false, "rewrite internal/api/testdata/dashboard_golden.json")

// captureDashboardPayload serves a single dashboard request against target and
// returns a JSON-marshalable snapshot with the one known-nondeterministic
// field (active-user lists, whose order comes from Go map iteration)
// normalized via sort.
func captureDashboardPayload(t *testing.T, server *Server, target string) map[string]interface{} {
	t.Helper()

	server.cache.Clear()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardData(rec, req)

	var decoded map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode dashboard payload for %s: %v", target, err)
	}

	if status, ok := decoded["status"].(map[string]interface{}); ok {
		for _, key := range []string{"active_users_wireguard_list", "active_users_singbox_list"} {
			if rawList, ok := status[key].([]interface{}); ok {
				strList := make([]string, 0, len(rawList))
				for _, v := range rawList {
					if s, ok := v.(string); ok {
						strList = append(strList, s)
					}
				}
				sort.Strings(strList)
				normalized := make([]interface{}, len(strList))
				for i, s := range strList {
					normalized[i] = s
				}
				status[key] = normalized
			}
		}
	}

	return map[string]interface{}{
		"status_code": rec.Code,
		"payload":     decoded,
	}
}

func TestDashboardPayloadGolden(t *testing.T) {
	server, _, _ := newDashboardTestServer(t)

	golden := map[string]interface{}{
		"fixed_window_default": captureDashboardPayload(t, server, "/api/dashboard?start=1&end=1000"),
		"fixed_window_wide":    captureDashboardPayload(t, server, "/api/dashboard?start=0&end=100000"),
	}

	// Structural assertion 1: wireguard_interfaces has exactly the wg0/wg1 keys
	// produced by the single GetWGKeyTotals query.
	defaultPayload := golden["fixed_window_default"].(map[string]interface{})["payload"].(map[string]interface{})
	wgIfaces, ok := defaultPayload["wireguard_interfaces"].(map[string]interface{})
	if !ok {
		t.Fatalf("wireguard_interfaces missing or wrong type in payload")
	}
	wantIfaceKeys := []string{"wg0", "wg1"}
	gotIfaceKeys := make([]string, 0, len(wgIfaces))
	for k := range wgIfaces {
		gotIfaceKeys = append(gotIfaceKeys, k)
	}
	sort.Strings(gotIfaceKeys)
	if fmt.Sprint(gotIfaceKeys) != fmt.Sprint(wantIfaceKeys) {
		t.Fatalf("wireguard_interfaces keys = %v, want %v", gotIfaceKeys, wantIfaceKeys)
	}

	// Structural assertion 2: status has exactly the 8-key contract.
	status, ok := defaultPayload["status"].(map[string]interface{})
	if !ok {
		t.Fatalf("status missing or wrong type in payload")
	}
	wantStatusKeys := []string{
		"active_users_singbox", "active_users_singbox_list",
		"active_users_wireguard", "active_users_wireguard_list",
		"enable_singbox", "enable_wireguard", "singbox", "wireguard",
	}
	gotStatusKeys := make([]string, 0, len(status))
	for k := range status {
		gotStatusKeys = append(gotStatusKeys, k)
	}
	sort.Strings(gotStatusKeys)
	sortedWant := append([]string(nil), wantStatusKeys...)
	sort.Strings(sortedWant)
	if fmt.Sprint(gotStatusKeys) != fmt.Sprint(sortedWant) {
		t.Fatalf("status keys = %v, want %v", gotStatusKeys, sortedWant)
	}

	got, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/dashboard_golden.json"

	if *updateDashGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -updatedashgolden first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch:\n%s", diffDashboardLines(got, want, 40))
	}
}

func diffDashboardLines(got, want []byte, n int) string {
	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(string(want), "\n")
	var b strings.Builder
	shown := 0
	max := len(gotLines)
	if len(wantLines) > max {
		max = len(wantLines)
	}
	for i := 0; i < max && shown < n; i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			fmt.Fprintf(&b, "line %d:\n  got:  %s\n  want: %s\n", i+1, g, w)
			shown++
		}
	}
	return b.String()
}
