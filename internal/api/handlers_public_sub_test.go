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

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

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
	if len(lines) != 1 {
		t.Fatalf("decoded body should contain a single canonical link, got %q", decoded)
	}
	if !strings.HasPrefix(lines[0], "vless://11111111-1111-1111-1111-111111111111@sub.example.com:443") {
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
	if got[0].Name != "History Bundle" {
		t.Fatalf("name=%q want %q", got[0].Name, "History Bundle")
	}
	if got[0].UserName != "alice" {
		t.Fatalf("user_name=%q want %q", got[0].UserName, "alice")
	}
	if got[0].RequestIP != "198.51.100.5" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIP, "198.51.100.5")
	}
	if got[0].RequestHost != "swg.example.com" {
		t.Fatalf("request_host=%q want %q", got[0].RequestHost, "swg.example.com")
	}
	if got[0].RequestPath != "/s/[token]" {
		t.Fatalf("request_path=%q want %q", got[0].RequestPath, "/s/[token]")
	}
	if got[0].UserAgent != "v2raytun/ios" {
		t.Fatalf("user_agent=%q want %q", got[0].UserAgent, "v2raytun/ios")
	}
	if got[0].DeviceModel != "iPhone 14 Pro Max" {
		t.Fatalf("device_model=%q want %q", got[0].DeviceModel, "iPhone 14 Pro Max")
	}
	if got[0].DeviceOS != "iOS" {
		t.Fatalf("device_os=%q want %q", got[0].DeviceOS, "iOS")
	}
	if got[0].DeviceOSVersion != "26.4" {
		t.Fatalf("device_os_version=%q want %q", got[0].DeviceOSVersion, "26.4")
	}
	if got[0].AppVersion != "2.4.4" {
		t.Fatalf("app_version=%q want %q", got[0].AppVersion, "2.4.4")
	}
	if got[0].Country != "AR" {
		t.Fatalf("country=%q want %q", got[0].Country, "AR")
	}
	if got[0].HwidPrefix != "1226BDD7" {
		t.Fatalf("hwid_prefix=%q want %q", got[0].HwidPrefix, "1226BDD7")
	}
	if got[0].HwidHash != hashSubscriptionHWID("1226BDD7-30DF-409A-9FE7-C9CBCABC2335") {
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
	if got[0].RequestIP != "198.51.100.143" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIP, "198.51.100.143")
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
	if got[0].RequestIP != "2001:db8::143" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIP, "2001:db8::143")
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
	if got[0].RequestIP != "***" {
		t.Fatalf("request_ip=%q want %q", got[0].RequestIP, "***")
	}
	if got[0].RequestHost != "" || got[0].RequestPath != "" || got[0].UserAgent != "" || got[0].DeviceModel != "" || got[0].DeviceOS != "" || got[0].DeviceOSVersion != "" || got[0].AppVersion != "" || got[0].Country != "" || got[0].HwidHash != "" || got[0].HwidPrefix != "" {
		t.Fatalf("expected sensitive request metadata to be censored, got %+v", got[0])
	}
}
