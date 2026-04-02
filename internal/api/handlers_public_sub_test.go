package api

import (
	"encoding/base64"
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

func newPublicSubscriptionTestServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	dataStore, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	stub := &singboxConfigExecutorStub{data: []byte(`{
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
	}`)}

	cfg := &core.Config{
		EnableSingbox:     true,
		PublicIP:          "sub.example.com",
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   []string{"test-vless"},
		StatsInbounds:     []string{"test-vless"},
	}
	cfg.SetExecutor(stub)

	return NewServer(dataStore, cfg, stub), dataStore
}

func TestHandlePublicSubscription_SetsProfileTitleAndKeepsItOnCachedResponses(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "sub-token",
		Name:        "Alpha Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		ResetDay:    1,
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
}

func TestHandlePublicSubscription_SetsQuotaResetExpireForZeroUsage(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.now = func() time.Time {
		return time.Date(2026, time.April, 2, 10, 30, 0, 0, time.UTC)
	}

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "quota-token",
		Name:        "Quota Bundle",
		QuotaLimit:  10 * 1024 * 1024 * 1024,
		QuotaPeriod: "monthly",
		ResetDay:    1,
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

	wantExpire := strconv.FormatInt(nextQuotaResetUnix(server.now(), "monthly", 1), 10)
	if !strings.Contains(userinfo, "expire="+wantExpire) {
		t.Fatalf("Subscription-Userinfo=%q want expire=%s", userinfo, wantExpire)
	}
}
