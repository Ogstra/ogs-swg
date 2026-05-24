package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// newCacheTestServer builds a *Server with an in-memory SQLite store backed
// by a sing-box config that already contains one user ("alice") in a managed
// inbound. This gives handleGetUsers a non-empty list to cache.
func newCacheTestServer(t *testing.T) *Server {
	t.Helper()
	server, _ := newPublicSubscriptionTestServer(t)
	return server
}

// newInboundMetaCacheTestServer returns a server backed by a store that has
// one inbound_meta row seeded for "test-vless".
func newInboundMetaCacheTestServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()
	server, stub, store := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
				],
				"tls": {"enabled": true, "server_name": "example.com"},
				"transport": {"type": "tcp"}
			}
		],
		"experimental": {
			"v2ray_api": {
				"listen": "127.0.0.1:19001",
				"stats": {
					"enabled": true,
					"inbounds": ["test-vless"],
					"outbounds": ["direct"],
					"users": []
				}
			}
		}
	}`)
	_ = stub
	if err := store.SaveInboundMeta(core.InboundMeta{Tag: "test-vless", ExternalPort: 7443}); err != nil {
		t.Fatalf("SaveInboundMeta: %v", err)
	}
	return server, store
}

// newRouteTagCacheTestServer returns a server with sing-box route rules and one
// seeded route tag linked to the first rule.
func newRouteTagCacheTestServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()
	server, _, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
	return server, store
}

func assertCacheFound(t *testing.T, s *Server, key string) {
	t.Helper()
	s.cache.Wait()
	if _, found := s.cache.Get(key); !found {
		t.Fatalf("expected %s to be cached", key)
	}
}

func assertCacheMissing(t *testing.T, s *Server, key string) {
	t.Helper()
	if _, found := s.cache.Get(key); found {
		t.Fatalf("expected %s to be evicted", key)
	}
}

func seedCacheKey(t *testing.T, s *Server, key string) {
	t.Helper()
	s.cache.SetWithTTL(key, []byte("cached"), 1, time.Minute)
	s.cache.Wait()
}

func primeSubscriptionsCache(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetSubscriptions(rec, httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetSubscriptions: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllSubscriptions)
}

func primeHappConfigCache(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetSubscriptionHappConfig(rec, httptest.NewRequest(http.MethodGet, "/api/subscriptions/happ-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetSubscriptionHappConfig: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyHappConfig)
}

func createCacheTestSubscription(t *testing.T, s *Server, name string) subscriptionCreateResponse {
	t.Helper()
	return createSubscriptionForTest(t, s, subscriptionMutationRequest{
		Name:        name,
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})
}

func primeInboundMetaCache(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetSingboxInbounds(rec, httptest.NewRequest(http.MethodGet, "/api/singbox/inbounds", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetSingboxInbounds: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllInboundMeta)
}

func primeRouteTagsCache(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetUserRouteTags(rec, httptest.NewRequest(http.MethodGet, "/api/user-route-tags", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetUserRouteTags: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllRouteTags)
}

func primePanelUsersCache(t *testing.T, s *Server) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGetPanelUsers(rec, httptest.NewRequest(http.MethodGet, "/api/panel-users", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleGetPanelUsers: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllPanelUsers)
}

func premiumRouteRule() map[string]interface{} {
	return map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geoip-premium"},
		"outbound":  "premium",
		"auth_user": []interface{}{"alice"},
		"x-extra":   map[string]interface{}{"preserve": true},
	}
}

// TestGetUsers_CacheHit verifies that the second GET /api/users call is served
// from the cache: both responses must have identical JSON bodies, and after the
// first call s.cache.Get(cacheKeyAllUsers) must return found=true.
func TestGetUsers_CacheHit(t *testing.T) {
	s := newCacheTestServer(t)

	// First call — populates cache.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	s.handleGetUsers(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first GET /api/users: status=%d body=%q", rec1.Code, rec1.Body.String())
	}
	s.cache.Wait()

	// Assert cache was populated.
	_, found := s.cache.Get(cacheKeyAllUsers)
	if !found {
		t.Fatal("expected cacheKeyAllUsers to be set after first GET /api/users, but cache.Get returned found=false")
	}

	// Second call — must be served from cache.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	s.handleGetUsers(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second GET /api/users: status=%d body=%q", rec2.Code, rec2.Body.String())
	}

	// Both responses must decode to identical JSON.
	var got1, got2 []interface{}
	if err := json.NewDecoder(rec1.Body).Decode(&got1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.NewDecoder(rec2.Body).Decode(&got2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	b1, _ := json.Marshal(got1)
	b2, _ := json.Marshal(got2)
	if string(b1) != string(b2) {
		t.Fatalf("cache hit response mismatch:\nfirst:  %s\nsecond: %s", b1, b2)
	}
}

// TestCreateUser_InvalidatesCache primes the cache via GET, calls handleCreateUser,
// then asserts the cache entry for cacheKeyAllUsers is gone.
func TestCreateUser_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)

	// Prime the cache.
	rec := httptest.NewRecorder()
	s.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	s.cache.Wait()

	if _, found := s.cache.Get(cacheKeyAllUsers); !found {
		t.Fatal("precondition: cache not populated before create")
	}

	writeRec := httptest.NewRecorder()
	s.handleCreateUser(writeRec, newJSONRequest(t, http.MethodPost, "/api/users", CreateUserRequest{
		Name:        "bob",
		UUID:        "22222222-2222-2222-2222-222222222222",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		InboundTag:  "test-vless",
	}))
	if writeRec.Code != http.StatusCreated {
		t.Fatalf("handleCreateUser: status=%d body=%q", writeRec.Code, writeRec.Body.String())
	}

	if _, found := s.cache.Get(cacheKeyAllUsers); found {
		t.Fatal("expected cacheKeyAllUsers to be evicted after handleCreateUser, but it is still present")
	}
}

// TestUpdateUser_InvalidatesCache primes the cache via GET, calls handleUpdateUser,
// then asserts the cache entry for cacheKeyAllUsers is gone.
func TestUpdateUser_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)

	rec := httptest.NewRecorder()
	s.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	s.cache.Wait()

	if _, found := s.cache.Get(cacheKeyAllUsers); !found {
		t.Fatal("precondition: cache not populated before update")
	}

	writeRec := httptest.NewRecorder()
	s.handleUpdateUser(writeRec, newJSONRequest(t, http.MethodPut, "/api/users/alice", CreateUserRequest{
		Name:        "alice",
		UUID:        "11111111-1111-1111-1111-111111111111",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		InboundTag:  "test-vless",
	}))
	if writeRec.Code != http.StatusOK {
		t.Fatalf("handleUpdateUser: status=%d body=%q", writeRec.Code, writeRec.Body.String())
	}

	if _, found := s.cache.Get(cacheKeyAllUsers); found {
		t.Fatal("expected cacheKeyAllUsers to be evicted after handleUpdateUser, but it is still present")
	}
}

// TestDeleteUser_InvalidatesCache primes the cache via GET, calls handleDeleteUser,
// then asserts the cache entry for cacheKeyAllUsers is gone.
func TestDeleteUser_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)

	rec := httptest.NewRecorder()
	s.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	s.cache.Wait()

	if _, found := s.cache.Get(cacheKeyAllUsers); !found {
		t.Fatal("precondition: cache not populated before delete")
	}

	writeRec := httptest.NewRecorder()
	writeRec2 := httptest.NewRequest(http.MethodDelete, "/api/users?name=alice", nil)
	s.handleDeleteUser(writeRec, writeRec2)
	if writeRec.Code != http.StatusOK {
		t.Fatalf("handleDeleteUser: status=%d body=%q", writeRec.Code, writeRec.Body.String())
	}

	if _, found := s.cache.Get(cacheKeyAllUsers); found {
		t.Fatal("expected cacheKeyAllUsers to be evicted after handleDeleteUser, but it is still present")
	}
}

// TestBulkCreateUsers_InvalidatesCache primes the cache via GET, calls handleBulkCreateUsers,
// then asserts the cache entry for cacheKeyAllUsers is gone.
func TestBulkCreateUsers_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)

	rec := httptest.NewRecorder()
	s.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	s.cache.Wait()

	if _, found := s.cache.Get(cacheKeyAllUsers); !found {
		t.Fatal("precondition: cache not populated before bulk create")
	}

	writeRec := httptest.NewRecorder()
	s.handleBulkCreateUsers(writeRec, newJSONRequest(t, http.MethodPost, "/api/users/bulk", []CreateUserRequest{
		{
			Name:        "charlie",
			UUID:        "33333333-3333-3333-3333-333333333333",
			QuotaLimit:  0,
			QuotaPeriod: "monthly",
			InboundTag:  "test-vless",
		},
	}))
	if writeRec.Code != http.StatusCreated {
		t.Fatalf("handleBulkCreateUsers: status=%d body=%q", writeRec.Code, writeRec.Body.String())
	}

	if _, found := s.cache.Get(cacheKeyAllUsers); found {
		t.Fatal("expected cacheKeyAllUsers to be evicted after handleBulkCreateUsers, but it is still present")
	}
}

// TestUpdateUserRouteTags_InvalidatesUsersCache primes the cache via GET, calls
// handleUpdateUserRouteTags, then asserts the cache entry is gone. Route-tag
// updates invalidate the users cache because the users response embeds
// derived route_tags membership.
func TestUpdateUserRouteTags_InvalidatesUsersCache(t *testing.T) {
	s := newCacheTestServer(t)

	rec := httptest.NewRecorder()
	s.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	s.cache.Wait()

	if _, found := s.cache.Get(cacheKeyAllUsers); !found {
		t.Fatal("precondition: cache not populated before route-tag update")
	}

	writeRec := httptest.NewRecorder()
	writeReq := newJSONRequest(t, http.MethodPut, "/api/users/alice/route-tags", UpdateUserRouteTagsRequest{
		TagIDs: []int64{},
	})
	writeReq.SetPathValue("name", "alice")
	s.handleUpdateUserRouteTags(writeRec, writeReq)
	if writeRec.Code != http.StatusOK {
		t.Fatalf("handleUpdateUserRouteTags: status=%d body=%q", writeRec.Code, writeRec.Body.String())
	}

	if _, found := s.cache.Get(cacheKeyAllUsers); found {
		t.Fatal("expected cacheKeyAllUsers to be evicted after handleUpdateUserRouteTags, but it is still present")
	}
}

func TestGetSubscriptions_CacheHit(t *testing.T) {
	s := newCacheTestServer(t)
	createCacheTestSubscription(t, s, "Cache Bundle")
	primeSubscriptionsCache(t, s)

	rec := httptest.NewRecorder()
	s.handleGetSubscriptions(rec, httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second handleGetSubscriptions: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllSubscriptions)
}

func TestCreateSubscription_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	createCacheTestSubscription(t, s, "Existing Bundle")
	primeSubscriptionsCache(t, s)

	createCacheTestSubscription(t, s, "Created Bundle")
	assertCacheMissing(t, s, cacheKeyAllSubscriptions)
}

func TestUpdateSubscription_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	created := createCacheTestSubscription(t, s, "Original Bundle")
	primeSubscriptionsCache(t, s)

	updateSubscriptionForTest(t, s, created.ID, subscriptionMutationRequest{
		Name:        "Updated Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})
	assertCacheMissing(t, s, cacheKeyAllSubscriptions)
}

func TestDeleteSubscription_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	created := createCacheTestSubscription(t, s, "Delete Bundle")
	primeSubscriptionsCache(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/subscriptions/"+strconv.FormatInt(created.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	s.handleDeleteSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleDeleteSubscription: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllSubscriptions)
}

func TestRegenerateSubscriptionToken_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	created := createCacheTestSubscription(t, s, "Token Bundle")
	primeSubscriptionsCache(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/"+strconv.FormatInt(created.ID, 10)+"/regenerate-token", nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	s.handleRegenerateSubscriptionToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleRegenerateSubscriptionToken: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllSubscriptions)
}

func TestGetHappConfig_CacheHit(t *testing.T) {
	s := newCacheTestServer(t)
	primeHappConfigCache(t, s)

	rec := httptest.NewRecorder()
	s.handleGetSubscriptionHappConfig(rec, httptest.NewRequest(http.MethodGet, "/api/subscriptions/happ-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second handleGetSubscriptionHappConfig: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyHappConfig)
}

func TestUpdateHappConfig_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	primeHappConfigCache(t, s)

	rec := httptest.NewRecorder()
	s.handleUpdateSubscriptionHappConfig(rec, newJSONRequest(t, http.MethodPut, "/api/subscriptions/happ-config", SubscriptionHappConfigRequest{
		ProviderID: "cache-provider",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdateSubscriptionHappConfig: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyHappConfig)
}

func TestGetSingboxInbounds_CacheHit(t *testing.T) {
	s, _ := newInboundMetaCacheTestServer(t)
	primeInboundMetaCache(t, s)

	rec := httptest.NewRecorder()
	s.handleGetSingboxInbounds(rec, httptest.NewRequest(http.MethodGet, "/api/singbox/inbounds", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second handleGetSingboxInbounds: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllInboundMeta)
}

func TestUpdateSingboxInbound_InvalidatesMetaCache(t *testing.T) {
	s, _ := newInboundMetaCacheTestServer(t)
	primeInboundMetaCache(t, s)

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPut, "/api/singbox/inbound?tag=test-vless", map[string]interface{}{
		"type":                "vless",
		"tag":                 "test-vless",
		"listen":              "0.0.0.0",
		"listen_port":         float64(443),
		"external_port":       float64(8443),
		"link_allow_insecure": "auto",
		"users": []interface{}{
			map[string]interface{}{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111"},
		},
	})
	s.handleUpdateSingboxInbound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdateSingboxInbound: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllInboundMeta)
}

func TestDeleteSingboxInbound_InvalidatesMetaCache(t *testing.T) {
	s, _ := newInboundMetaCacheTestServer(t)
	primeInboundMetaCache(t, s)
	seedCacheKey(t, s, cacheKeyAllUsers)

	rec := httptest.NewRecorder()
	s.handleDeleteSingboxInbound(rec, httptest.NewRequest(http.MethodDelete, "/api/singbox/inbound?tag=test-vless", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleDeleteSingboxInbound: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllInboundMeta)
	assertCacheMissing(t, s, cacheKeyAllUsers)
}

func TestGetUserRouteTags_CacheHit(t *testing.T) {
	s, store := newRouteTagCacheTestServer(t)
	createRouteTagForRule(t, store, premiumRouteRule(), "Premium")
	primeRouteTagsCache(t, s)

	rec := httptest.NewRecorder()
	s.handleGetUserRouteTags(rec, httptest.NewRequest(http.MethodGet, "/api/user-route-tags", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second handleGetUserRouteTags: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllRouteTags)
}

func TestCreateUserRouteTag_InvalidatesCache(t *testing.T) {
	s, store := newRouteTagCacheTestServer(t)
	createRouteTagForRule(t, store, premiumRouteRule(), "Premium")
	primeRouteTagsCache(t, s)
	seedCacheKey(t, s, cacheKeyAllUsers)

	rec := httptest.NewRecorder()
	s.handleCreateUserRouteTag(rec, newJSONRequest(t, http.MethodPost, "/api/user-route-tags", map[string]interface{}{
		"name":       "Empty",
		"color":      "#0088ff",
		"rule_index": float64(1),
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("handleCreateUserRouteTag: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllRouteTags)
	assertCacheMissing(t, s, cacheKeyAllUsers)
}

func TestUpdateUserRouteTag_InvalidatesCache(t *testing.T) {
	s, store := newRouteTagCacheTestServer(t)
	tag := createRouteTagForRule(t, store, premiumRouteRule(), "Premium")
	primeRouteTagsCache(t, s)
	seedCacheKey(t, s, cacheKeyAllUsers)

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPut, "/api/user-route-tags/"+strconv.FormatInt(tag.ID, 10), map[string]interface{}{
		"name":  "Premium Renamed",
		"color": "#00aa00",
	})
	req.SetPathValue("id", strconv.FormatInt(tag.ID, 10))
	s.handleUpdateUserRouteTag(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdateUserRouteTag: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllRouteTags)
	assertCacheMissing(t, s, cacheKeyAllUsers)
}

func TestDeleteUserRouteTag_InvalidatesCache(t *testing.T) {
	s, store := newRouteTagCacheTestServer(t)
	tag := createRouteTagForRule(t, store, premiumRouteRule(), "Premium")
	primeRouteTagsCache(t, s)
	seedCacheKey(t, s, cacheKeyAllUsers)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/user-route-tags/"+strconv.FormatInt(tag.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(tag.ID, 10))
	s.handleDeleteUserRouteTag(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("handleDeleteUserRouteTag: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllRouteTags)
	assertCacheMissing(t, s, cacheKeyAllUsers)
}

func TestGetPanelUsers_CacheHit(t *testing.T) {
	s := newCacheTestServer(t)
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	s.handleGetPanelUsers(rec, httptest.NewRequest(http.MethodGet, "/api/panel-users", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second handleGetPanelUsers: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheFound(t, s, cacheKeyAllPanelUsers)
}

func TestCreatePanelUser_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	s.handleCreatePanelUser(rec, newJSONRequest(t, http.MethodPost, "/api/panel-users", createPanelUserRequest{
		Username: "cache-create",
		Password: "password123",
		Permissions: core.PanelUserPermissions{
			CanReadUsers: true,
		},
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("handleCreatePanelUser: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllPanelUsers)
}

func TestUpdatePanelUserPermissions_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	if err := s.store.CreatePanelUser("cache-perms", "password123", core.PanelUserPermissions{}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	s.handleUpdatePanelUserPermissions(rec, newJSONRequest(t, http.MethodPut, "/api/panel-users/permissions", updatePanelUserPermissionsRequest{
		Username:    "cache-perms",
		Permissions: core.PanelUserPermissions{CanReadUsers: true},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdatePanelUserPermissions: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllPanelUsers)
}

func TestUpdatePanelUserUsername_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	if err := s.store.CreatePanelUser("cache-rename", "password123", core.PanelUserPermissions{}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	s.handleUpdatePanelUserUsername(rec, newJSONRequest(t, http.MethodPut, "/api/panel-users/username", updatePanelUserUsernameRequest{
		Username:    "cache-rename",
		NewUsername: "cache-renamed",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdatePanelUserUsername: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllPanelUsers)
}

func TestUpdatePanelUserPassword_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	if err := s.store.CreatePanelUser("cache-password", "password123", core.PanelUserPermissions{}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	s.handleUpdatePanelUserPassword(rec, newJSONRequest(t, http.MethodPut, "/api/panel-users/password", updatePanelUserPasswordRequest{
		Username:    "cache-password",
		NewPassword: "newpass123",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdatePanelUserPassword: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllPanelUsers)
}

func TestDeletePanelUser_InvalidatesCache(t *testing.T) {
	s := newCacheTestServer(t)
	if err := s.store.CreatePanelUser("cache-delete", "password123", core.PanelUserPermissions{}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}
	primePanelUsersCache(t, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/panel-users?username=cache-delete", nil)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, map[string]interface{}{"sub": "admin"}))
	s.handleDeletePanelUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleDeletePanelUser: status=%d body=%q", rec.Code, rec.Body.String())
	}
	assertCacheMissing(t, s, cacheKeyAllPanelUsers)
}
