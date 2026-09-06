package api

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// updateSubGolden rewrites internal/api/testdata/subscription_golden.json from
// the current (unmodified) behavior of handlePublicSubscription. Run with:
//
//	go test ./internal/api -run TestPublicSubscriptionGolden -updatesubgolden -count=1
var updateSubGolden = flag.Bool("updatesubgolden", false, "rewrite internal/api/testdata/subscription_golden.json")

const subscriptionGoldenConfigJSON = `{
	"inbounds": [
		{
			"type": "vless",
			"tag": "test-vless",
			"listen": "0.0.0.0",
			"listen_port": 443,
			"tls": {
				"enabled": true,
				"server_name": "edge.example.com",
				"reality": {
					"enabled": true,
					"handshake": {"server": "edge.example.com", "server_port": 443},
					"private_key": "reality-private-key-placeholder",
					"public_key": "reality-public-key-placeholder",
					"short_id": ["deadbeef"]
				}
			},
			"users": [
				{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111", "flow": "xtls-rprx-vision"},
				{"name": "bob", "uuid": "33333333-3333-3333-3333-333333333333", "flow": "xtls-rprx-vision"}
			]
		},
		{
			"type": "hysteria2",
			"tag": "test-hy2",
			"listen": "0.0.0.0",
			"listen_port": 4443,
			"tls": {
				"enabled": true,
				"server_name": "edge.example.com"
			},
			"obfs": {
				"type": "salamander",
				"password": "obfs-password-placeholder"
			},
			"users": [
				{"name": "carol", "password": "hy2-password-placeholder"}
			]
		}
	],
	"experimental": {
		"v2ray_api": {
			"listen": "127.0.0.1:19001",
			"stats": {
				"enabled": true,
				"inbounds": ["test-vless"],
				"outbounds": ["direct"],
				"users": ["alice", "bob"]
			}
		}
	}
}`

// newSubscriptionGoldenServer builds a deterministic subscription server fixture
// covering multi-user aggregation, sub-level quota, external profile links, and
// Happ metadata, pinned to a fixed clock.
func newSubscriptionGoldenServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()

	server, dataStore := newPublicSubscriptionTestServerWithConfig(t, subscriptionGoldenConfigJSON, []string{"test-vless", "test-hy2"})

	server.now = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	server.config.SubscriptionProtection = core.SubscriptionProtectionConfig{}

	if err := dataStore.SaveUserMetadata(core.UserMetadata{Email: "alice", QuotaLimit: 5 * 1024 * 1024 * 1024, QuotaPeriod: "monthly", Enabled: true}); err != nil {
		t.Fatalf("SaveUserMetadata alice: %v", err)
	}
	if err := dataStore.SaveUserMetadata(core.UserMetadata{Email: "bob", QuotaLimit: 3 * 1024 * 1024 * 1024, QuotaPeriod: "monthly", Enabled: true}); err != nil {
		t.Fatalf("SaveUserMetadata bob: %v", err)
	}
	if err := dataStore.SaveUserMetadata(core.UserMetadata{Email: "carol", QuotaLimit: 0, QuotaPeriod: "monthly", Enabled: true}); err != nil {
		t.Fatalf("SaveUserMetadata carol: %v", err)
	}

	sampleTS := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC).Unix()
	if err := dataStore.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: sampleTS, Uplink: 1024, Downlink: 2048},
		{User: "bob", Timestamp: sampleTS, Uplink: 4096, Downlink: 8192},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	profileID, err := dataStore.UpsertExternalProfile(core.ExternalProfile{
		Name:       "homelab",
		Type:       "vless",
		Enabled:    true,
		HostIPv4:   "198.51.100.20",
		Port:       8443,
		UUID:       "22222222-2222-2222-2222-222222222222",
		PublicKey:  "public-key-placeholder",
		ShortID:    "beef",
		ServerName: "sni.example.com",
		ALPN:       "h2",
		Flow:       "xtls-rprx-vision",
		Flag:       "[EU] ",
	})
	if err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}
	if err := dataStore.SetUserExternalProfiles("bob", []int64{profileID}); err != nil {
		t.Fatalf("SetUserExternalProfiles: %v", err)
	}

	createSub := func(token, name, alias string, quotaLimit int64, members []string, extra func(*store.CreateSubscriptionParams)) {
		params := store.CreateSubscriptionParams{
			Token:       token,
			Name:        name,
			Alias:       alias,
			QuotaLimit:  sql.NullInt64{Int64: quotaLimit, Valid: true},
			QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
			ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
		}
		if extra != nil {
			extra(&params)
		}
		subID, err := dataStore.Queries.CreateSubscription(t.Context(), params)
		if err != nil {
			t.Fatalf("CreateSubscription(%s): %v", token, err)
		}
		for i, member := range members {
			if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
				SubID:    subID,
				UserName: member,
				Position: int64(i),
			}); err != nil {
				t.Fatalf("AddUserToSubscription(%s, %s): %v", token, member, err)
			}
		}
	}

	createSub("single-token", "Single", "", 0, []string{"alice"}, nil)
	createSub("multi-token", "Multi", "Multi Alias", 0, []string{"alice", "bob"}, nil)
	createSub("subquota-token", "SubQuota", "", 10*1024*1024*1024, []string{"alice", "bob"}, nil)
	createSub("happ-token", "Happ", "", 0, []string{"alice"}, func(p *store.CreateSubscriptionParams) {
		p.ProfileUpdateIntervalHours = sql.NullInt64{Int64: 12, Valid: true}
		p.HappRoutingProfile = `{"Name":"p","DirectSites":["a.example.com"]}`
		p.HappDirectSites = "b.example.com,c.example.com"
	})
	createSub("hy2-token", "Hy2", "", 0, []string{"carol"}, nil)

	return server, dataStore
}

// captureSubResponse serves a single request against target (a full "/s/..."
// path including any query string) and returns a JSON-marshalable snapshot of
// the status, headers (excluding Date) and decoded body.
func captureSubResponse(t *testing.T, server *Server, target string, mutate func(*http.Request)) map[string]interface{} {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	token := strings.TrimPrefix(target, "/s/")
	if idx := strings.IndexAny(token, "?"); idx >= 0 {
		token = token[:idx]
	}
	req.SetPathValue("token", token)
	if mutate != nil {
		mutate(req)
	}

	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	headers := map[string][]string{}
	for key, values := range rec.Header() {
		if key == "Date" {
			continue
		}
		sorted := append([]string(nil), values...)
		sort.Strings(sorted)
		headers[key] = sorted
	}

	bodyStr := rec.Body.String()
	var decodedLines []string
	if decoded, err := base64.StdEncoding.DecodeString(bodyStr); err != nil {
		decodedLines = []string{"ERROR: not base64", bodyStr}
	} else {
		decodedLines = strings.Split(string(decoded), "\n")
	}

	return map[string]interface{}{
		"status":             rec.Code,
		"headers":            headers,
		"body_base64":        bodyStr,
		"body_decoded_lines": decodedLines,
	}
}

func TestPublicSubscriptionGolden(t *testing.T) {
	server, _ := newSubscriptionGoldenServer(t)

	golden := map[string]interface{}{}

	golden["single_plain"] = captureSubResponse(t, server, "/s/single-token", nil)
	golden["multi_aggregation"] = captureSubResponse(t, server, "/s/multi-token", nil)
	golden["multi_aggregation_cache_hit"] = captureSubResponse(t, server, "/s/multi-token", nil)
	golden["sub_level_quota"] = captureSubResponse(t, server, "/s/subquota-token", nil)
	golden["happ_client_query"] = captureSubResponse(t, server, "/s/happ-token?client=happ", nil)
	golden["happ_ua_headers"] = captureSubResponse(t, server, "/s/happ-token", func(r *http.Request) {
		r.Header.Set("User-Agent", "Happ/2.0")
		r.Header.Set("X-Hwid", "hwid-placeholder")
	})
	golden["shadowrocket_ua"] = captureSubResponse(t, server, "/s/single-token", func(r *http.Request) {
		r.Header.Set("User-Agent", "Shadowrocket/1.0")
	})
	golden["hysteria2_single"] = captureSubResponse(t, server, "/s/hy2-token", nil)

	if !reflect.DeepEqual(golden["multi_aggregation"], golden["multi_aggregation_cache_hit"]) {
		t.Fatalf("multi_aggregation and multi_aggregation_cache_hit must be identical (cached-response path)")
	}

	got, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/subscription_golden.json"

	if *updateSubGolden {
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
		t.Fatalf("read golden (run with -updatesubgolden first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch:\n%s", diffFirstNLines(got, want, 40))
	}
}

func diffFirstNLines(got, want []byte, n int) string {
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
