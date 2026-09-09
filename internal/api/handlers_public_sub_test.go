package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

func addProtectionRuleForTest(t *testing.T, s *Server, ruleType string, value string) {
	t.Helper()

	if _, err := s.store.Queries.InsertProtectionRule(t.Context(), store.InsertProtectionRuleParams{
		RuleType:  ruleType,
		Value:     value,
		Note:      "test",
		CreatedAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	}); err != nil {
		t.Fatalf("InsertProtectionRule: %v", err)
	}
	s.reloadProtectionRules(t.Context())
}

func serveAuthedRequest(server *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	server.AuthMiddleware(server.Routes()).ServeHTTP(rec, req)
	return rec
}

func withSettingsPerms(r *http.Request, perms *core.PanelUserPermissions) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), permissionsContextKey, perms))
}

func newPublicSubscriptionTestServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()

	return newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"tls": {
					"enabled": true,
					"server_name": "edge.example.com"
				},
				"users": [
					{
						"name": "alice",
						"uuid": "11111111-1111-1111-1111-111111111111"
					}
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
					"users": ["alice"]
				}
			}
		}
	}`, []string{"test-vless"})
}

func newPublicSubscriptionTestServerWithConfig(t *testing.T, initialJSON string, managedInbounds []string) (*Server, *core.Store) {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	dataStore, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	stub := &singboxConfigExecutorStub{data: []byte(initialJSON)}

	cfg := &core.Config{
		EnableSingbox:     true,
		PublicIP:          "sub.example.com",
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   managedInbounds,
		StatsInbounds:     managedInbounds,
	}
	cfg.SetExecutor(stub)

	return NewServer(dataStore, cfg, stub), dataStore
}

func TestHandlePublicSubscription_SetsProfileTitleAndKeepsItOnCachedResponses(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "sub-token",
		Name:        "Alpha Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/s/sub-token", nil)
		req.SetPathValue("token", "sub-token")
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		return rec
	}

	first := makeRequest()
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%q", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Profile-Title"); got != "Alpha Bundle" {
		t.Fatalf("first Profile-Title=%q want %q", got, "Alpha Bundle")
	}
	if got := first.Header().Get("Subscription-Userinfo"); got != "upload=0; download=0; total=0" {
		t.Fatalf("first Subscription-Userinfo=%q", got)
	}
	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(first.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(string(body), "vless://11111111-1111-1111-1111-111111111111@sub.example.com:443") {
		t.Fatalf("decoded body missing expected link: %q", string(body))
	}

	second := makeRequest()
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%q", second.Code, second.Body.String())
	}
	if got := second.Header().Get("Profile-Title"); got != "Alpha Bundle" {
		t.Fatalf("second Profile-Title=%q want %q", got, "Alpha Bundle")
	}

	detail := getSubscriptionForTest(t, server, subID)
	if detail.LastRequestAt == nil || *detail.LastRequestAt <= 0 {
		t.Fatalf("last_request_at=%v want non-nil positive timestamp", detail.LastRequestAt)
	}
}

func TestHandlePublicSubscription_UsesMemberAliasInDeliveredProfileName(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "alias-token",
		Name:        "Alias Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
		Alias:    "Alice Phone",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/alias-token", nil)
	req.SetPathValue("token", "alias-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}

	decoded := string(body)
	if !strings.Contains(decoded, "#Alice+Phone") {
		t.Fatalf("decoded body=%q want alias-based link label", decoded)
	}
	if strings.Contains(decoded, "#alice") {
		t.Fatalf("decoded body=%q should not keep canonical username as delivered label when alias exists", decoded)
	}
}

func TestHandlePublicSubscription_HappParamsOnlyOnHappVariant(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "happ-token",
		Name:        "Happ Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}
	if err := dataStore.UpdateSubscriptionHappConfig(t.Context(), core.SubscriptionHappConfig{
		ProviderID:       "provider-test-id",
		HideSettings:     "1",
		AlwaysHWID:       "1",
		AutoUpdateOnOpen: "0",
		PingOnOpen:       "1",
		ColorProfile:     `{"theme":"violet"}`,
		RoutingProfile:   `{"Name":"global","GlobalProxy":"true","FakeDNS":"true"}`,
		AdvancedParameters: []core.SubscriptionHappParameter{
			{Key: "fallback-url", Value: "https://fallback.example.com/s"},
			{Key: "subscription-autoconnect", Value: "1"},
			{Key: "ping-type", Value: "proxy"},
		},
	}); err != nil {
		t.Fatalf("UpdateSubscriptionHappConfig: %v", err)
	}

	directReq := httptest.NewRequest(http.MethodGet, "/s/happ-token", nil)
	directReq.SetPathValue("token", "happ-token")
	directRec := httptest.NewRecorder()
	server.handlePublicSubscription(directRec, directReq)
	if directRec.Code != http.StatusOK {
		t.Fatalf("direct status=%d body=%q", directRec.Code, directRec.Body.String())
	}
	if got := directRec.Header().Get("providerid"); got != "" {
		t.Fatalf("direct providerid header=%q want empty", got)
	}
	directBody, err := base64.StdEncoding.DecodeString(strings.TrimSpace(directRec.Body.String()))
	if err != nil {
		t.Fatalf("decode direct body: %v", err)
	}
	if strings.Contains(string(directBody), "providerid") || strings.Contains(string(directBody), "hide-settings") {
		t.Fatalf("direct decoded body should not include Happ params: %q", string(directBody))
	}

	happReq := httptest.NewRequest(http.MethodGet, "/s/happ-token?client=happ", nil)
	happReq.SetPathValue("token", "happ-token")
	happRec := httptest.NewRecorder()
	server.handlePublicSubscription(happRec, happReq)
	if happRec.Code != http.StatusOK {
		t.Fatalf("happ status=%d body=%q", happRec.Code, happRec.Body.String())
	}
	if got := happRec.Header().Get("providerid"); got != "provider-test-id" {
		t.Fatalf("happ providerid header=%q", got)
	}
	if got := happRec.Header().Get("hide-settings"); got != "1" {
		t.Fatalf("happ hide-settings header=%q", got)
	}
	if got := happRec.Header().Get("subscription-autoconnect"); got != "1" {
		t.Fatalf("happ subscription-autoconnect header=%q", got)
	}
	if got := happRec.Header().Get("subscription-always-hwid-enable"); got != "1" {
		t.Fatalf("happ subscription-always-hwid-enable header=%q", got)
	}
	if got := happRec.Header().Get("subscription-auto-update-open-enable"); got != "0" {
		t.Fatalf("happ subscription-auto-update-open-enable header=%q", got)
	}
	if got := happRec.Header().Get("subscription-ping-onopen-enabled"); got != "1" {
		t.Fatalf("happ subscription-ping-onopen-enabled header=%q", got)
	}
	if got := happRec.Header().Get("color-profile"); got != `{"theme":"violet"}` {
		t.Fatalf("happ color-profile header=%q", got)
	}
	if got := happRec.Header().Get("fallback-url"); got != "https://fallback.example.com/s/happ-token" {
		t.Fatalf("happ fallback-url header=%q", got)
	}
	happBody, err := base64.StdEncoding.DecodeString(strings.TrimSpace(happRec.Body.String()))
	if err != nil {
		t.Fatalf("decode happ body: %v", err)
	}
	decoded := string(happBody)
	for _, want := range []string{
		"#providerid provider-test-id",
		"#hide-settings: 1",
		"#subscription-always-hwid-enable: 1",
		"#subscription-auto-update-open-enable: 0",
		"#subscription-ping-onopen-enabled: 1",
		`#color-profile: {"theme":"violet"}`,
		"#fallback-url: https://fallback.example.com/s/happ-token",
		"#subscription-autoconnect: 1",
		"#ping-type: proxy",
		"vless://11111111-1111-1111-1111-111111111111@sub.example.com:443",
	} {
		if !strings.Contains(decoded, want) {
			t.Fatalf("decoded Happ body missing %q: %q", want, decoded)
		}
	}
	if strings.Contains(decoded, "#routing:") {
		t.Fatalf("decoded Happ body should not include #routing prefix: %q", decoded)
	}
	wantRoutingJSON := `{"Name":"global","GlobalProxy":"true","FakeDNS":"true"}`
	wantRouting := "happ://routing/onadd/" + base64.StdEncoding.EncodeToString([]byte(wantRoutingJSON))
	if !strings.Contains(decoded, wantRouting) {
		t.Fatalf("decoded Happ body missing routing link %q: %q", wantRouting, decoded)
	}

	// Test User-Agent triggering Happ profile
	happUAReq := httptest.NewRequest(http.MethodGet, "/s/happ-token", nil)
	happUAReq.SetPathValue("token", "happ-token")
	happUAReq.Header.Set("User-Agent", "Happ/1.0.0 (iOS)")
	happUARec := httptest.NewRecorder()
	server.handlePublicSubscription(happUARec, happUAReq)
	if got := happUARec.Header().Get("providerid"); got != "provider-test-id" {
		t.Fatalf("happ UA providerid header=%q", got)
	}

	// Happ-compatible clients may identify themselves with device/HWID headers
	// even when the User-Agent is absent or normalized by a proxy.
	happHWIDReq := httptest.NewRequest(http.MethodGet, "/s/happ-token", nil)
	happHWIDReq.SetPathValue("token", "happ-token")
	happHWIDReq.Header.Set("X-Hwid", "test-hwid")
	happHWIDRec := httptest.NewRecorder()
	server.handlePublicSubscription(happHWIDRec, happHWIDReq)
	if got := happHWIDRec.Header().Get("providerid"); got != "provider-test-id" {
		t.Fatalf("happ HWID providerid header=%q", got)
	}
	happHWIDBody, err := base64.StdEncoding.DecodeString(strings.TrimSpace(happHWIDRec.Body.String()))
	if err != nil {
		t.Fatalf("decode happ HWID body: %v", err)
	}
	if got := string(happHWIDBody); !strings.Contains(got, "#providerid provider-test-id") {
		t.Fatalf("happ HWID body missing providerid: %q", got)
	}
}

func TestHandlePublicSubscription_HappRoutingOffEmitsDisableLink(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:              "happ-off-token",
		Name:               "Happ Off Bundle",
		QuotaLimit:         sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod:        sql.NullString{String: "monthly", Valid: true},
		ResetDay:           sql.NullInt64{Int64: 1, Valid: true},
		HappRoutingProfile: "happ://routing/off",
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}
	if err := dataStore.UpdateSubscriptionHappConfig(t.Context(), core.SubscriptionHappConfig{
		ProviderID: "provider-test-id",
	}); err != nil {
		t.Fatalf("UpdateSubscriptionHappConfig: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/happ-off-token?client=happ", nil)
	req.SetPathValue("token", "happ-off-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	decoded := string(body)
	if !strings.Contains(decoded, "happ://routing/off") {
		t.Fatalf("decoded body missing routing off link: %q", decoded)
	}
	if strings.Contains(decoded, "happ://routing/onadd/") {
		t.Fatalf("decoded body should not include onadd routing link: %q", decoded)
	}
}

func TestHandlePublicSubscription_PreservesSubscriptionMemberOrder(t *testing.T) {
	server, _ := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"tls": {
					"enabled": true,
					"server_name": "edge.example.com"
				},
				"users": [
					{
						"name": "alice",
						"uuid": "11111111-1111-1111-1111-111111111111"
					},
					{
						"name": "bob",
						"uuid": "22222222-2222-2222-2222-222222222222"
					}
				]
			}
		]
	}`, []string{"test-vless"})

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Ordered Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Members: []subscriptionMemberPayload{
			{Username: "bob", Alias: ""},
			{Username: "alice", Alias: ""},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/s/"+created.Token, nil)
	req.SetPathValue("token", created.Token)
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	allLines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var lines []string
	for _, l := range allLines {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("decoded lines=%q want two links", string(body))
	}
	if !strings.Contains(lines[0], "22222222-2222-2222-2222-222222222222") || !strings.Contains(lines[1], "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("decoded body order=%q want bob before alice", string(body))
	}
}

func TestHandlePublicSubscription_UsesSingleCanonicalInboundForLegacyUsers(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"tls": {
					"enabled": true,
					"server_name": "edge.example.com"
				},
				"users": [
					{
						"name": "alice",
						"uuid": "11111111-1111-1111-1111-111111111111"
					}
				]
			},
			{
				"type": "trojan",
				"tag": "test-trojan",
				"listen": "0.0.0.0",
				"listen_port": 8443,
				"users": [
					{
						"name": "alice",
						"password": "legacy-password"
					}
				]
			}
		],
		"experimental": {
			"v2ray_api": {
				"listen": "127.0.0.1:19001",
				"stats": {
					"enabled": true,
					"inbounds": ["test-vless", "test-trojan"],
					"outbounds": ["direct"],
					"users": ["alice"]
				}
			}
		}
	}`, []string{"test-vless", "test-trojan"})

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "legacy-sub-token",
		Name:        "Legacy Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/legacy-sub-token", nil)
	req.SetPathValue("token", "legacy-sub-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	decoded := strings.TrimSpace(string(body))
	lines := strings.Split(decoded, "\n")
	var linkLines []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "#") {
			linkLines = append(linkLines, l)
		}
	}
	if len(linkLines) != 1 {
		t.Fatalf("decoded body should contain a single canonical link, got %q", decoded)
	}
	if !strings.HasPrefix(linkLines[0], "vless://11111111-1111-1111-1111-111111111111@sub.example.com:443") {
		t.Fatalf("decoded body = %q; want canonical vless link only", decoded)
	}
}

func TestHandlePublicSubscription_OmitsExpireWithoutExplicitExpiration(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "quota-token",
		Name:        "Quota Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 10 * 1024 * 1024 * 1024, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/quota-token", nil)
	req.SetPathValue("token", "quota-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	userinfo := rec.Header().Get("Subscription-Userinfo")
	if !strings.Contains(userinfo, "upload=0; download=0; total=10737418240") {
		t.Fatalf("Subscription-Userinfo=%q", userinfo)
	}
	if strings.Contains(userinfo, "expire=") {
		t.Fatalf("Subscription-Userinfo=%q should omit expire without explicit expiration", userinfo)
	}
}

func TestHandlePublicSubscription_IPAllowlistBypass(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.MaxRequests = 1
	server.config.SubscriptionProtection.WindowSeconds = 60

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "allow-token",
		Name:        "Allow Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	addProtectionRuleForTest(t, server, "ip_allow", "198.51.100.5")
	server.subscriptionLimiter.record("allow-token")

	req := httptest.NewRequest(http.MethodGet, "/s/allow-token", nil)
	req.SetPathValue("token", "allow-token")
	req.RemoteAddr = "198.51.100.5:12345"
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_IPBlock(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "ip-block-token",
		Name:        "IP Block Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	addProtectionRuleForTest(t, server, "ip_block", "198.51.100.5")

	req := httptest.NewRequest(http.MethodGet, "/s/ip-block-token", nil)
	req.SetPathValue("token", "ip-block-token")
	req.RemoteAddr = "198.51.100.5:12345"
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_TokenBlock(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "token-block-token",
		Name:        "Token Block Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	addProtectionRuleForTest(t, server, "token_block", "token-block-token")

	req := httptest.NewRequest(http.MethodGet, "/s/token-block-token", nil)
	req.SetPathValue("token", "token-block-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_RateLimit(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.MaxRequests = 1
	server.config.SubscriptionProtection.WindowSeconds = 60

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "rate-limit-token",
		Name:        "Rate Limit Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/s/rate-limit-token", nil)
	firstReq.SetPathValue("token", "rate-limit-token")
	firstRec := httptest.NewRecorder()
	server.handlePublicSubscription(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%q", firstRec.Code, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/s/rate-limit-token", nil)
	secondReq.SetPathValue("token", "rate-limit-token")
	secondRec := httptest.NewRecorder()
	server.handlePublicSubscription(secondRec, secondReq)

	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%q", secondRec.Code, secondRec.Body.String())
	}
	if secondRec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header")
	}
}

func TestHandlePublicSubscription_UAFilter_Enabled(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.UAFilterEnabled = true

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "ua-filter-token",
		Name:        "UA Filter Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/ua-filter-token", nil)
	req.SetPathValue("token", "ua-filter-token")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_UAFilter_Disabled(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "ua-disabled-token",
		Name:        "UA Disabled Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/ua-disabled-token", nil)
	req.SetPathValue("token", "ua-disabled-token")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_BlockedRequestRecorded(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "blocked-record-token",
		Name:        "Blocked Record Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	addProtectionRuleForTest(t, server, "token_block", "blocked-record-token")

	req := httptest.NewRequest(http.MethodGet, "/s/blocked-record-token", nil)
	req.SetPathValue("token", "blocked-record-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	rows, err := dataStore.Queries.GetBlockedSubscriptionRequests(t.Context(), store.GetBlockedSubscriptionRequestsParams{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("GetBlockedSubscriptionRequests: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected blocked request rows")
	}
	if rows[0].BlockReason != "token_block" {
		t.Fatalf("block_reason=%q want %q", rows[0].BlockReason, "token_block")
	}
	if rows[0].SubID != subID {
		t.Fatalf("sub_id=%d want %d", rows[0].SubID, subID)
	}
}

func TestSubscriptionProtectionSettingsRoutes(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)
	server.config.APIKey = "settings-key"
	server.config.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	server.config.SubscriptionProtection = core.SubscriptionProtectionConfig{
		MaxRequests:   60,
		WindowSeconds: 60,
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/subscription-protection", nil)
	getReq.Header.Set("X-API-Key", "settings-key")
	getRec := serveAuthedRequest(server, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", getRec.Code, getRec.Body.String())
	}

	var current core.SubscriptionProtectionConfig
	if err := json.NewDecoder(getRec.Body).Decode(&current); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if current.MaxRequests != 60 || current.WindowSeconds != 60 || current.UAFilterEnabled || current.SocialFetchersBlockEnabled {
		t.Fatalf("unexpected initial config: %+v", current)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings/subscription-protection", strings.NewReader(`{"max_requests":7,"window_seconds":45,"ua_filter_enabled":true,"social_fetchers_block_enabled":true}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("X-API-Key", "settings-key")
	putRec := serveAuthedRequest(server, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%q", putRec.Code, putRec.Body.String())
	}

	var updated core.SubscriptionProtectionConfig
	if err := json.NewDecoder(putRec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode put response: %v", err)
	}
	if updated.MaxRequests != 7 || updated.WindowSeconds != 45 || !updated.UAFilterEnabled || !updated.SocialFetchersBlockEnabled {
		t.Fatalf("unexpected updated config: %+v", updated)
	}
	if server.config.SubscriptionProtection != updated {
		t.Fatalf("server config not updated: %+v", server.config.SubscriptionProtection)
	}
}

func TestSubscriptionProtectionRuleRoutes(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.APIKey = "settings-key"

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "blocked-log-token",
		Name:        "Blocked Log Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.InsertSubscriptionRequest(t.Context(), store.InsertSubscriptionRequestParams{
		SubID:           subID,
		UserName:        "alice",
		RequestIp:       "198.51.100.77",
		RequestHost:     "sub.example.com",
		RequestPath:     "/s/[token]",
		UserAgent:       "Clash/Meta/1.0",
		DeviceModel:     "PC",
		DeviceOs:        "Windows",
		DeviceOsVersion: "11",
		AppVersion:      "1.0",
		Country:         "US",
		HwidHash:        "",
		HwidPrefix:      "",
		RequestedAt:     time.Now().Unix(),
		ServedFromCache: 0,
		Blocked:         1,
		BlockReason:     "ip_block",
	}); err != nil {
		t.Fatalf("InsertSubscriptionRequest: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/settings/protection-rules", strings.NewReader(`{"rule_type":"ip_block","value":"198.51.100.5","note":"test note"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-API-Key", "settings-key")
	createRec := serveAuthedRequest(server, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%q", createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/settings/protection-rules", http.NoBody)
	listReq.Header.Set("X-API-Key", "settings-key")
	listRec := serveAuthedRequest(server, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%q", listRec.Code, listRec.Body.String())
	}

	var rules []store.SubscriptionProtectionRule
	if err := json.NewDecoder(listRec.Body).Decode(&rules); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules=%d want 1", len(rules))
	}
	if rules[0].RuleType != "ip_block" || rules[0].Value != "198.51.100.5" || rules[0].Note != "test note" {
		t.Fatalf("unexpected rule: %+v", rules[0])
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/api/settings/protection-rules/blocked-log?limit=10&offset=0", http.NoBody)
	blockedReq.Header.Set("X-API-Key", "settings-key")
	blockedRec := serveAuthedRequest(server, blockedReq)
	if blockedRec.Code != http.StatusOK {
		t.Fatalf("blocked-log status=%d body=%q", blockedRec.Code, blockedRec.Body.String())
	}

	var blocked []store.GetBlockedSubscriptionRequestsRow
	if err := json.NewDecoder(blockedRec.Body).Decode(&blocked); err != nil {
		t.Fatalf("decode blocked-log: %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked-log rows=%d want 1", len(blocked))
	}
	if blocked[0].SubID != subID || blocked[0].BlockReason != "ip_block" || blocked[0].RequestIp != "198.51.100.77" {
		t.Fatalf("unexpected blocked row: %+v", blocked[0])
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/settings/protection-rules/"+strconv.FormatInt(rules[0].ID, 10), http.NoBody)
	deleteReq.Header.Set("X-API-Key", "settings-key")
	deleteRec := serveAuthedRequest(server, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%q", deleteRec.Code, deleteRec.Body.String())
	}

	listAfterDeleteReq := httptest.NewRequest(http.MethodGet, "/api/settings/protection-rules", http.NoBody)
	listAfterDeleteReq.Header.Set("X-API-Key", "settings-key")
	listAfterDeleteRec := serveAuthedRequest(server, listAfterDeleteReq)
	if listAfterDeleteRec.Code != http.StatusOK {
		t.Fatalf("list-after-delete status=%d body=%q", listAfterDeleteRec.Code, listAfterDeleteRec.Body.String())
	}
	rules = nil
	if err := json.NewDecoder(listAfterDeleteRec.Body).Decode(&rules); err != nil {
		t.Fatalf("decode rules after delete: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules after delete=%d want 0", len(rules))
	}
}

func TestHandlePublicSubscription_UAFilter_AllowsKnownClientUA(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.UAFilterEnabled = true

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "client-ua-token",
		Name:        "Client UA Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/client-ua-token", nil)
	req.SetPathValue("token", "client-ua-token")
	req.Header.Set("User-Agent", "Mozilla/5.0 Shadowrocket/2306 CFNetwork/1410.0 Darwin/22.0.0")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_SocialFetchersFilter_Enabled(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.SocialFetchersBlockEnabled = true

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "social-fetcher-token",
		Name:        "Social Fetcher Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/social-fetcher-token", nil)
	req.SetPathValue("token", "social-fetcher-token")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	rows, err := dataStore.Queries.GetBlockedSubscriptionRequests(t.Context(), store.GetBlockedSubscriptionRequestsParams{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("GetBlockedSubscriptionRequests: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected blocked request rows")
	}
	if rows[0].BlockReason != "ua_social_fetcher" {
		t.Fatalf("block_reason=%q want %q", rows[0].BlockReason, "ua_social_fetcher")
	}
}

func TestHandlePublicSubscription_SocialFetchersFilter_Disabled(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "social-fetcher-disabled-token",
		Name:        "Social Fetcher Disabled Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/social-fetcher-disabled-token", nil)
	req.SetPathValue("token", "social-fetcher-disabled-token")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlePublicSubscription_SocialFetcherDoesNotConsumeRateLimitQuota(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	// Set a tight rate limit of 1 request per minute and enable social fetcher blocking.
	server.config.SubscriptionProtection.MaxRequests = 1
	server.config.SubscriptionProtection.WindowSeconds = 60
	server.config.SubscriptionProtection.SocialFetchersBlockEnabled = true

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "sf-quota-token",
		Name:        "SF Quota Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	// Send a social fetcher request (simulating WhatsApp preview bot).
	// It must be blocked with 403, NOT consume a rate-limit slot.
	for i := range 8 {
		req := httptest.NewRequest(http.MethodGet, "/s/sf-quota-token", nil)
		req.SetPathValue("token", "sf-quota-token")
		req.Header.Set("User-Agent", "WhatsApp/2.23.20.0 A")
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("social fetcher request %d: status=%d, want 403", i+1, rec.Code)
		}
	}

	// After 8 social fetcher hits, a legitimate proxy client request must still succeed
	// because the social fetcher requests must not have consumed any rate-limit quota.
	legitReq := httptest.NewRequest(http.MethodGet, "/s/sf-quota-token", nil)
	legitReq.SetPathValue("token", "sf-quota-token")
	legitReq.Header.Set("User-Agent", "ClashMeta/1.18.0")
	legitRec := httptest.NewRecorder()
	server.handlePublicSubscription(legitRec, legitReq)
	if legitRec.Code != http.StatusOK {
		t.Fatalf("legitimate client after social fetcher hits: status=%d body=%q (social fetcher requests must not consume rate-limit quota)", legitRec.Code, legitRec.Body.String())
	}
}

func TestHandlePublicSubscription_EmitsRefreshPolicyHeaders(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	interval := int64(24)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:                      "refresh-token",
		Name:                       "Refresh Bundle",
		QuotaLimit:                 sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod:                sql.NullString{String: "monthly", Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: sql.NullInt64{Int64: interval, Valid: true},
		UpdateAlways:               1,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/refresh-token", nil)
	req.SetPathValue("token", "refresh-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if got := rec.Header().Get("profile-update-interval"); got != "24" {
		t.Fatalf("profile-update-interval=%q want %q", got, "24")
	}
	if got := rec.Header().Get("update-always"); got != "true" {
		t.Fatalf("update-always=%q want %q", got, "true")
	}
}

func TestHandlePublicSubscription_InvalidatesCachedRefreshPolicyAfterSubscriptionUpdate(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	initialInterval := int64(24)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:                      "cached-refresh-token",
		Name:                       "Cached Refresh Bundle",
		QuotaLimit:                 sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod:                sql.NullString{String: "monthly", Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: sql.NullInt64{Int64: initialInterval, Valid: true},
		UpdateAlways:               0,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/s/cached-refresh-token", nil)
	firstReq.SetPathValue("token", "cached-refresh-token")
	firstRec := httptest.NewRecorder()
	server.handlePublicSubscription(firstRec, firstReq)

	if got := firstRec.Header().Get("profile-update-interval"); got != "24" {
		t.Fatalf("first profile-update-interval=%q want %q", got, "24")
	}
	if got := firstRec.Header().Get("update-always"); got != "" {
		t.Fatalf("first update-always=%q want empty", got)
	}

	body, err := json.Marshal(map[string]any{
		"name":                          "Cached Refresh Bundle",
		"quota_limit":                   int64(0),
		"quota_period":                  "monthly",
		"users":                         []string{"alice"},
		"profile_update_interval_hours": int64(12),
		"update_always":                 true,
	})
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/subscriptions/"+strconv.FormatInt(subID, 10), strings.NewReader(string(body)))
	updateReq.SetPathValue("id", strconv.FormatInt(subID, 10))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	server.handleUpdateSubscription(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", updateRec.Code, updateRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/s/cached-refresh-token", nil)
	secondReq.SetPathValue("token", "cached-refresh-token")
	secondRec := httptest.NewRecorder()
	server.handlePublicSubscription(secondRec, secondReq)

	if got := secondRec.Header().Get("profile-update-interval"); got != "12" {
		t.Fatalf("second profile-update-interval=%q want %q", got, "12")
	}
	if got := secondRec.Header().Get("update-always"); got != "true" {
		t.Fatalf("second update-always=%q want %q", got, "true")
	}
}

func TestHandlePublicSubscription_OmitsIntervalHeaderAfterDisablingViaUpdate(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	initialInterval := int64(24)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:                      "disable-interval-token",
		Name:                       "Disable Interval Bundle",
		QuotaLimit:                 sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod:                sql.NullString{String: "monthly", Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: sql.NullInt64{Int64: initialInterval, Valid: true},
		UpdateAlways:               0,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	// First fetch: interval header should be present and cached.
	firstReq := httptest.NewRequest(http.MethodGet, "/s/disable-interval-token", nil)
	firstReq.SetPathValue("token", "disable-interval-token")
	firstRec := httptest.NewRecorder()
	server.handlePublicSubscription(firstRec, firstReq)

	if got := firstRec.Header().Get("profile-update-interval"); got != "24" {
		t.Fatalf("first profile-update-interval=%q want %q", got, "24")
	}

	// Update subscription: disable the interval by sending explicit null.
	body, err := json.Marshal(map[string]any{
		"name":                          "Disable Interval Bundle",
		"quota_limit":                   int64(0),
		"quota_period":                  "monthly",
		"users":                         []string{"alice"},
		"profile_update_interval_hours": nil,
		"update_always":                 false,
	})
	if err != nil {
		t.Fatalf("marshal update body: %v", err)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/subscriptions/"+strconv.FormatInt(subID, 10), strings.NewReader(string(body)))
	updateReq.SetPathValue("id", strconv.FormatInt(subID, 10))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	server.handleUpdateSubscription(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", updateRec.Code, updateRec.Body.String())
	}

	// Second fetch: interval header must be absent.
	secondReq := httptest.NewRequest(http.MethodGet, "/s/disable-interval-token", nil)
	secondReq.SetPathValue("token", "disable-interval-token")
	secondRec := httptest.NewRecorder()
	server.handlePublicSubscription(secondRec, secondReq)

	if got := secondRec.Header().Get("profile-update-interval"); got != "0" {
		t.Fatalf("after disabling: profile-update-interval=%q want %q", got, "0")
	}
}

func TestHandleSubscriptionRequestHistory_ReturnsRequesterMetadata(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-token",
		Name:        "History Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-token", nil)
	req.SetPathValue("token", "history-token")
	req.RemoteAddr = "198.51.100.5:12345"
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "v2raytun/ios")
	req.Header.Set("X-Device-Model", "iPhone 14 Pro Max")
	req.Header.Set("X-Device-OS", "iOS")
	req.Header.Set("X-Ver-Os", "26.4")
	req.Header.Set("X-App-Version", "2.4.4")
	req.Header.Set("CF-IPCountry", "AR")
	req.Header.Set("X-Hwid", "1226BDD7-30DF-409A-9FE7-C9CBCABC2335")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].Name != "History Bundle" { // Corrected field name
		t.Fatalf("name=%q want %q", got[0].Name, "History Bundle")
	}
	if got[0].UserName != "alice" { // Corrected field name
		t.Fatalf("user_name=%q want %q", got[0].UserName, "alice")
	}
	if got[0].RequestIp != "198.51.100.5" { // Corrected field name
		t.Fatalf("request_ip=%q want %q", got[0].RequestIp, "198.51.100.5")
	}
	if got[0].RequestHost != "swg.example.com" { // Corrected field name
		t.Fatalf("request_host=%q want %q", got[0].RequestHost, "swg.example.com")
	}
	if got[0].RequestPath != "/s/[token]" { // Corrected field name
		t.Fatalf("request_path=%q want %q", got[0].RequestPath, "/s/[token]")
	}
	if got[0].UserAgent != "v2raytun/ios" { // Corrected field name
		t.Fatalf("user_agent=%q want %q", got[0].UserAgent, "v2raytun/ios")
	}
	if got[0].DeviceModel != "iPhone 14 Pro Max" { // Corrected field name
		t.Fatalf("device_model=%q want %q", got[0].DeviceModel, "iPhone 14 Pro Max")
	}
	if got[0].DeviceOs != "iOS" { // Corrected field name
		t.Fatalf("device_os=%q want %q", got[0].DeviceOs, "iOS")
	}
	if got[0].DeviceOsVersion != "26.4" { // Corrected field name
		t.Fatalf("device_os_version=%q want %q", got[0].DeviceOsVersion, "26.4")
	}
	if got[0].AppVersion != "2.4.4" { // Corrected field name
		t.Fatalf("app_version=%q want %q", got[0].AppVersion, "2.4.4")
	}
	if got[0].Country != "AR" { // Corrected field name
		t.Fatalf("country=%q want %q", got[0].Country, "AR")
	}
	if got[0].HwidPrefix != "1226BDD7" { // Corrected field name
		t.Fatalf("hwid_prefix=%q want %q", got[0].HwidPrefix, "1226BDD7")
	}
	if got[0].HwidHash != hashSubscriptionHWID("1226BDD7-30DF-409A-9FE7-C9CBCABC2335") { // Corrected field name
		t.Fatalf("hwid_hash=%q want hash of x-hwid", got[0].HwidHash)
	}
}

func TestExtractSubscriptionRequestMetadata_FallsBackToParsedAppleUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/s/history-token", nil)
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "Shadowrocket/3082 CFNetwork/3860.500.112 Darwin/25.4.0 iPhone15,3")
	req.Header.Set("CF-IPCountry", "AR")

	got := extractSubscriptionRequestMetadata(req)

	if got.userAgent != "Shadowrocket/3082 CFNetwork/3860.500.112 Darwin/25.4.0 iPhone15,3" {
		t.Fatalf("userAgent=%q", got.userAgent)
	}
	if got.deviceModel != "iPhone 14 Pro Max" {
		t.Fatalf("deviceModel=%q want %q", got.deviceModel, "iPhone 14 Pro Max")
	}
	if got.deviceOS != "iOS" {
		t.Fatalf("deviceOS=%q want %q", got.deviceOS, "iOS")
	}
	if got.deviceOSVersion != "" {
		t.Fatalf("deviceOSVersion=%q want empty", got.deviceOSVersion)
	}
	if got.appVersion != "3082" {
		t.Fatalf("appVersion=%q want %q", got.appVersion, "3082")
	}
	if got.country != "AR" {
		t.Fatalf("country=%q want %q", got.country, "AR")
	}
}

func TestExtractSubscriptionRequestMetadata_FallsBackToParsedSamsungUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/s/history-token", nil)
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "ClashMetaForAndroid/2.11.5 Android 14; SM-S918B Build/UP1A.231005.007")

	got := extractSubscriptionRequestMetadata(req)

	if got.deviceModel != "Samsung SM-S918B" {
		t.Fatalf("deviceModel=%q want %q", got.deviceModel, "Samsung SM-S918B")
	}
	if got.deviceOS != "Android" {
		t.Fatalf("deviceOS=%q want %q", got.deviceOS, "Android")
	}
	if got.deviceOSVersion != "14" {
		t.Fatalf("deviceOSVersion=%q want %q", got.deviceOSVersion, "14")
	}
	if got.appVersion != "2.11.5" {
		t.Fatalf("appVersion=%q want %q", got.appVersion, "2.11.5")
	}
}

func TestExtractSubscriptionRequestMetadata_FallsBackToParsedDesktopClientUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/s/history-token", nil)
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "Proxor/PC/1.5.0 (Prefer ClashMeta Format)")
	req.Header.Set("CF-IPCountry", "AR")

	got := extractSubscriptionRequestMetadata(req)

	if got.deviceModel != "PC" {
		t.Fatalf("deviceModel=%q want %q", got.deviceModel, "PC")
	}
	if got.deviceOS != "" {
		t.Fatalf("deviceOS=%q want empty", got.deviceOS)
	}
	if got.deviceOSVersion != "" {
		t.Fatalf("deviceOSVersion=%q want empty", got.deviceOSVersion)
	}
	if got.appVersion != "1.5.0" {
		t.Fatalf("appVersion=%q want %q", got.appVersion, "1.5.0")
	}
	if got.country != "AR" {
		t.Fatalf("country=%q want %q", got.country, "AR")
	}
}

func TestExtractSubscriptionRequestMetadata_FallsBackToParsedBrowserUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/s/history-token", nil)
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")
	req.Header.Set("CF-IPCountry", "AR")

	got := extractSubscriptionRequestMetadata(req)

	if got.deviceModel != "Mac" {
		t.Fatalf("deviceModel=%q want %q", got.deviceModel, "Mac")
	}
	if got.deviceOS != "macOS" {
		t.Fatalf("deviceOS=%q want %q", got.deviceOS, "macOS")
	}
	if got.deviceOSVersion != "10.15.7" {
		t.Fatalf("deviceOSVersion=%q want %q", got.deviceOSVersion, "10.15.7")
	}
	if got.appVersion != "146.0.0.0" {
		t.Fatalf("appVersion=%q want %q", got.appVersion, "146.0.0.0")
	}
}

func TestResolveSubscriptionDeviceModel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "apple iphone identifier", input: "iPhone15,3", want: "iPhone 14 Pro Max"},
		{name: "apple ipad identifier", input: "iPad13,18", want: "iPad (10th generation)"},
		{name: "apple mac identifier", input: "Mac15,12", want: "MacBook Air (13-inch, M3, 2024)"},
		{name: "samsung code", input: "SM-S918B", want: "Samsung SM-S918B"},
		{name: "unknown passthrough", input: "Pixel 9 Pro", want: "Pixel 9 Pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveSubscriptionDeviceModel(tt.input); got != tt.want {
				t.Fatalf("resolveSubscriptionDeviceModel(%q)=%q want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleSubscriptionRequestHistory_ShowsCurrentSubscriptionUsers(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"tls": {
					"enabled": true,
					"server_name": "edge.example.com"
				},
				"users": [
					{
						"name": "alice",
						"uuid": "11111111-1111-1111-1111-111111111111"
					},
					{
						"name": "bob",
						"uuid": "22222222-2222-2222-2222-222222222222"
					}
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
	}`, []string{"test-vless"})

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-current-users-token",
		Name:        "History Current Users Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	for _, user := range []string{"alice", "bob"} {
		if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
			SubID:    subID,
			UserName: user,
		}); err != nil {
			t.Fatalf("AddUserToSubscription(%s): %v", user, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-current-users-token", nil)
	req.SetPathValue("token", "history-current-users-token")
	req.RemoteAddr = "198.51.100.5:12345"
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	if err := dataStore.Queries.RemoveUserFromSubscription(t.Context(), store.RemoveUserFromSubscriptionParams{
		SubID:    subID,
		UserName: "bob",
	}); err != nil {
		t.Fatalf("RemoveUserFromSubscription(bob): %v", err)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].UserName != "alice" {
		t.Fatalf("user_name=%q want %q", got[0].UserName, "alice")
	}
}

func TestHandleSubscriptionRequestHistory_FiltersUsersMissingFromCurrentConfig(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{
						"name": "alice",
						"uuid": "11111111-1111-1111-1111-111111111111"
					}
				]
			}
		]
	}`, []string{"test-vless"})

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-filter-missing-token",
		Name:        "History Filter Missing Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	for _, user := range []string{"alice", "bob"} {
		if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
			SubID:    subID,
			UserName: user,
		}); err != nil {
			t.Fatalf("AddUserToSubscription(%s): %v", user, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-filter-missing-token", nil)
	req.SetPathValue("token", "history-filter-missing-token")
	req.RemoteAddr = "198.51.100.5:12345"
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].UserName != "alice" {
		t.Fatalf("user_name=%q want %q", got[0].UserName, "alice")
	}
}

func TestHandleSubscriptionRequestHistory_PrefersPublicForwardedIPFromTrustedProxy(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-proxy-token",
		Name:        "History Proxy Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-proxy-token", nil)
	req.SetPathValue("token", "history-proxy-token")
	req.RemoteAddr = "172.18.0.3:8080"
	req.Header.Set("X-Real-IP", "172.18.0.3")
	req.Header.Set("X-Forwarded-For", "198.51.100.143, 172.18.0.3")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].RequestIp != "198.51.100.143" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIp, "198.51.100.143")
	}
}

func TestHandleSubscriptionRequestHistory_PreservesIPv6ForwardedIPFromTrustedProxy(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-ipv6-token",
		Name:        "History IPv6 Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-ipv6-token", nil)
	req.SetPathValue("token", "history-ipv6-token")
	req.RemoteAddr = "172.18.0.3:8080"
	req.Header.Set("X-Real-IP", "172.18.0.3")
	req.Header.Set("X-Forwarded-For", "2001:db8::143, 172.18.0.3")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].RequestIp != "2001:db8::143" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIp, "2001:db8::143")
	}
}

func TestHandleSubscriptionRequestHistory_CensorsSensitiveFieldsForRestrictedCallers(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "history-censored-token",
		Name:        "History Censored Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/history-censored-token", nil)
	req.SetPathValue("token", "history-censored-token")
	req.RemoteAddr = "198.51.100.9:2222"
	req.Host = "swg.example.com"
	req.Header.Set("User-Agent", "v2raytun/ios")
	req.Header.Set("X-Device-Model", "iPhone 14 Pro Max")
	req.Header.Set("X-Device-OS", "iOS")
	req.Header.Set("X-Ver-Os", "26.4")
	req.Header.Set("X-App-Version", "2.4.4")
	req.Header.Set("CF-IPCountry", "AR")
	req.Header.Set("X-Hwid", "1226BDD7-30DF-409A-9FE7-C9CBCABC2335")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true, CanReadLogsCensored: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].UserName != "Restricted" {
		t.Fatalf("user_name=%q want %q", got[0].UserName, "Restricted")
	}
	if got[0].RequestIp != "***" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIp, "***")
	}
	if got[0].RequestHost != "" || got[0].RequestPath != "" || got[0].UserAgent != "" || got[0].DeviceModel != "" || got[0].DeviceOs != "" || got[0].DeviceOsVersion != "" || got[0].AppVersion != "" || got[0].Country != "" || got[0].HwidHash != "" || got[0].HwidPrefix != "" {
		t.Fatalf("expected sensitive request metadata to be censored, got %+v", got[0])
	}
}

// TestBlockedSubscriptionRequest_DedupWithinWindow verifies that multiple blocked
// requests from the same IP for the same subscription and block reason within the
// dedup window are recorded only once. This guards against duplicate entries caused
// by parallel link-preview fetchers (e.g. WhatsApp) or rapid browser retries.
func TestBlockedSubscriptionRequest_DedupWithinWindow(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.config.SubscriptionProtection.SocialFetchersBlockEnabled = true

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "dedup-test-token",
		Name:        "Dedup Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	// Simulate three parallel requests from the same IP (as WhatsApp link-preview does).
	whatsappUA := "WhatsApp/2.23.1.79 A"
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/s/dedup-test-token", nil)
		req.SetPathValue("token", "dedup-test-token")
		req.Header.Set("User-Agent", whatsappUA)
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("request %d: status=%d body=%q", i, rec.Code, rec.Body.String())
		}
	}

	rows, err := dataStore.Queries.GetBlockedSubscriptionRequests(t.Context(), store.GetBlockedSubscriptionRequestsParams{
		Limit:  10,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("GetBlockedSubscriptionRequests: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 blocked record (dedup), got %d", len(rows))
	}
	if rows[0].BlockReason != "ua_social_fetcher" {
		t.Fatalf("block_reason=%q want %q", rows[0].BlockReason, "ua_social_fetcher")
	}
}

func TestHandlePublicSubscription_IncludesShadowsocksLink(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "shadowsocks",
				"tag": "test-ss",
				"listen": "0.0.0.0",
				"listen_port": 8443,
				"method": "2022-blake3-aes-128-gcm",
				"users": [
					{
						"name": "alice",
						"password": "shadow-pass"
					}
				]
			}
		]
	}`, []string{"test-ss"})

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "ss-sub-token",
		Name:        "SS Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/ss-sub-token", nil)
	req.SetPathValue("token", "ss-sub-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(string(body), "ss://") {
		t.Fatalf("subscription body missing ss:// link: %q", string(body))
	}
	if !strings.Contains(string(body), "@sub.example.com:8443") {
		t.Fatalf("subscription body missing host/port: %q", string(body))
	}
}

func TestHandlePublicSubscription_IncludesExternalOnlyProfile(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	externalID, err := dataStore.UpsertExternalProfile(core.ExternalProfile{
		Name:       "homelab-only",
		Flag:       "🇦🇷",
		Type:       "vless",
		HostIPv4:   "external.example.test",
		Port:       443,
		UUID:       "22222222-2222-2222-2222-222222222222",
		PublicKey:  "external-public-key",
		ShortID:    "abc123",
		ServerName: "sni.example.test",
		ALPN:       "h2,http/1.1",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "external-only-token",
		Name:        "External Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "homelab-only",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/external-only-token", nil)
	req.SetPathValue("token", "external-only-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	bodyText := string(body)
	if !strings.Contains(bodyText, "vless://22222222-2222-2222-2222-222222222222@external.example.test:443") {
		t.Fatalf("subscription body missing external profile %d link: %q", externalID, bodyText)
	}
	if !strings.Contains(bodyText, "#%F0%9F%87%A6%F0%9F%87%B7homelab-only") {
		t.Fatalf("subscription body missing external profile flag in display name: %q", bodyText)
	}
	if strings.Contains(bodyText, "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("subscription body unexpectedly included local alice link: %q", bodyText)
	}
}

func TestHandlePublicSubscription_UsesSubscriptionAliasInProfileTitle(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	// Create subscription with alias set
	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "pfm-alias-token",
		Name:        "canonical-name",
		Alias:       "Friendly Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/s/pfm-alias-token", nil)
	req.SetPathValue("token", "pfm-alias-token")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	// Profile-Title header must be the alias, not the canonical name
	if got := rec.Header().Get("Profile-Title"); got != "Friendly Bundle" {
		t.Fatalf("Profile-Title=%q want %q", got, "Friendly Bundle")
	}

	// Body must contain #profile-title: Friendly Bundle
	body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rec.Body.String()))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(string(body), "#profile-title: Friendly Bundle") {
		t.Fatalf("decoded body missing #profile-title line: %q", string(body))
	}

	// Empty alias falls back to canonical name
	subID2, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "pfm-noalias-token",
		Name:        "canonical-name-2",
		Alias:       "",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription no-alias: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID2,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/s/pfm-noalias-token", nil)
	req2.SetPathValue("token", "pfm-noalias-token")
	rec2 := httptest.NewRecorder()
	server.handlePublicSubscription(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("no-alias status=%d body=%q", rec2.Code, rec2.Body.String())
	}
	if got := rec2.Header().Get("Profile-Title"); got != "canonical-name-2" {
		t.Fatalf("no-alias Profile-Title=%q want %q", got, "canonical-name-2")
	}
}

func TestHandleSubscriptionRequestHistory_PrefersXCFClientIPFromWorker(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "worker-ip-token",
		Name:        "Worker IP Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	// Simulate Worker→nginx→panel: nginx overwrites CF-Connecting-IP and X-Real-IP
	// with the Worker egress IP. Worker sets X-CF-Client-IP to the real client IP.
	req := httptest.NewRequest(http.MethodGet, "/s/worker-ip-token", nil)
	req.SetPathValue("token", "worker-ip-token")
	req.RemoteAddr = "[2a06:98c0:3600::103]:12345"
	req.Header.Set("CF-Connecting-IP", "2a06:98c0:3600::103") // nginx-overwritten
	req.Header.Set("X-Real-IP", "2a06:98c0:3600::103")        // nginx-overwritten
	req.Header.Set("X-CF-Client-IP", "2001:db8::beef")        // set by Worker, untouched
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	if got[0].RequestIp != "2001:db8::beef" {
		t.Fatalf("request_ip=%q want %q (X-CF-Client-IP)", got[0].RequestIp, "2001:db8::beef")
	}
}

func TestHandleSubscriptionRequestHistory_PrefersCFConnectingIPOverRemoteAddr(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "cf-connecting-ip-token",
		Name:        "CF Connecting IP Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	// Simulate a Cloudflare-proxied request: RemoteAddr is the public Cloudflare
	// edge IP (not private, so isTrustedProxy returns false). CF-Connecting-IP
	// carries the real client IPv6.
	req := httptest.NewRequest(http.MethodGet, "/s/cf-connecting-ip-token", nil)
	req.SetPathValue("token", "cf-connecting-ip-token")
	req.RemoteAddr = "[2a06:98c0:3600::103]:12345"
	req.Header.Set("CF-Connecting-IP", "2001:db8::cafe")
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%q", rec.Code, rec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/subscription-requests/history?limit=5", nil)
	historyReq = withSettingsPerms(historyReq, &core.PanelUserPermissions{CanReadSettings: true, CanReadLogs: true})
	historyRec := httptest.NewRecorder()
	server.handleSubscriptionRequestHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%q", historyRec.Code, historyRec.Body.String())
	}

	var got []store.GetSubscriptionRequestHistoryRow
	decodeJSONResponse(t, historyRec, &got)
	if len(got) == 0 {
		t.Fatalf("history empty")
	}
	// Must record the real client IP from CF-Connecting-IP, not the Cloudflare edge IP.
	if got[0].RequestIp != "2001:db8::cafe" {
		t.Fatalf("request_ip=%q want %q (CF-Connecting-IP)", got[0].RequestIp, "2001:db8::cafe")
	}
}
