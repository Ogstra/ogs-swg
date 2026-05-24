package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newCacheTestServer builds a *Server with an in-memory SQLite store backed
// by a sing-box config that already contains one user ("alice") in a managed
// inbound. This gives handleGetUsers a non-empty list to cache.
func newCacheTestServer(t *testing.T) *Server {
	t.Helper()
	server, _ := newPublicSubscriptionTestServer(t)
	return server
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
