package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

const (
	migrationTestSrcToken = "single-token"
	migrationTestDstToken = "multi-token"
	migrationTestHWID     = "hwid-move-me"
)

// hitSub serves one Happ request against the public subscription handler.
func hitSub(t *testing.T, server *Server, token, hwid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
	req.SetPathValue("token", token)
	req.Header.Set("User-Agent", "Happ/2.7.1")
	if hwid != "" {
		req.Header.Set("X-Hwid", hwid)
	}
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /s/%s status = %d body=%q", token, rec.Code, rec.Body.String())
	}
	return rec
}

type migrationFixture struct {
	server *Server
	store  *core.Store
	srcID  int64
	dstID  int64
	hash   string
}

// newMigrationFixture arms a migration of migrationTestHWID from the golden
// "single" sub to the golden "multi" sub, with a provider id configured.
func newMigrationFixture(t *testing.T, advanced []core.SubscriptionHappParameter) *migrationFixture {
	t.Helper()
	server, st := newSubscriptionGoldenServer(t)
	ctx := context.Background()
	src, err := st.Queries.GetSubscriptionByToken(ctx, migrationTestSrcToken)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := st.Queries.GetSubscriptionByToken(ctx, migrationTestDstToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSubscriptionHappConfig(ctx, core.SubscriptionHappConfig{
		ProviderID:         "provider-placeholder",
		AdvancedParameters: advanced,
	}); err != nil {
		t.Fatal(err)
	}
	server.config.SubscriptionDomain = "sub.example.test"
	hash := hashSubscriptionHWID(migrationTestHWID)
	if err := st.CreateHWIDMigration(ctx, src.ID, hash, dst.ID, 1); err != nil {
		t.Fatal(err)
	}
	return &migrationFixture{server: server, store: st, srcID: src.ID, dstID: dst.ID, hash: hash}
}

func TestPublicSub_EmitsNewURLOnlyForMigratingHWID(t *testing.T) {
	f := newMigrationFixture(t, nil)

	moved := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if got := moved.Header().Get("new-url"); got != "https://sub.example.test/s/"+migrationTestDstToken {
		t.Fatalf("new-url = %q; want link to target sub", got)
	}

	if got := hitSub(t, f.server, migrationTestSrcToken, "hwid-someone-else").Header().Get("new-url"); got != "" {
		t.Fatalf("unrelated device got new-url %q", got)
	}
	if got := hitSub(t, f.server, migrationTestSrcToken, "").Header().Get("new-url"); got != "" {
		t.Fatalf("request without HWID got new-url %q", got)
	}
}

func TestPublicSub_NewURLNotBakedIntoCache(t *testing.T) {
	f := newMigrationFixture(t, nil)

	if got := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID).Header().Get("new-url"); got == "" {
		t.Fatal("migrating device got no new-url")
	}
	// Same token and Happ params: this request is a cache hit.
	other := hitSub(t, f.server, migrationTestSrcToken, "hwid-someone-else")
	if got := other.Header().Get("new-url"); got != "" {
		t.Fatalf("cached response leaked new-url %q to another device", got)
	}
	again := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if got := again.Header().Get("new-url"); got == "" {
		t.Fatal("migrating device lost new-url on a cache hit")
	}
}

func TestPublicSub_MigrationOverridesFallbackAndDropsNewDomain(t *testing.T) {
	f := newMigrationFixture(t, []core.SubscriptionHappParameter{
		{Key: "fallback-url", Value: "https://origin.example.test/s"},
		{Key: "new-domain", Value: "https://worker.example.test"},
	})
	want := "https://worker.example.test/s/" + migrationTestDstToken

	moved := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if got := moved.Header().Get("new-url"); got != want {
		t.Fatalf("new-url = %q; want %q (global new-domain host)", got, want)
	}
	if got := moved.Header().Get("fallback-url"); got != want {
		t.Fatalf("fallback-url = %q; want %q", got, want)
	}
	if got := moved.Header().Get("new-domain"); got != "" {
		t.Fatalf("new-domain = %q; want it removed for the migrating device", got)
	}

	other := hitSub(t, f.server, migrationTestSrcToken, "hwid-someone-else")
	if got := other.Header().Get("fallback-url"); got != "https://origin.example.test/s/"+migrationTestSrcToken {
		t.Fatalf("other device fallback-url = %q; want the global value", got)
	}
	if got := other.Header().Get("new-domain"); got != "https://worker.example.test" {
		t.Fatalf("other device new-domain = %q; want the global value", got)
	}
}

func TestPublicSub_NoNewURLWithoutProviderID(t *testing.T) {
	f := newMigrationFixture(t, nil)
	if err := f.store.UpdateSubscriptionHappConfig(context.Background(), core.SubscriptionHappConfig{}); err != nil {
		t.Fatal(err)
	}
	if got := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID).Header().Get("new-url"); got != "" {
		t.Fatalf("new-url = %q without a provider id", got)
	}
	m, _ := f.store.GetLiveHWIDMigration(context.Background(), f.srcID, f.hash)
	if m == nil || m.Attempts != 0 {
		t.Fatalf("migration = %#v; an undeliverable request must not count as an attempt", m)
	}
}

func TestPublicSub_MigratingDeviceGetsTargetProfilesInSameResponse(t *testing.T) {
	f := newMigrationFixture(t, nil)

	moved := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	target := hitSub(t, f.server, migrationTestDstToken, "hwid-someone-else")
	source := hitSub(t, f.server, migrationTestSrcToken, "hwid-someone-else")

	if source.Body.String() == target.Body.String() {
		t.Fatal("fixture source and target bodies are identical; the test would prove nothing")
	}
	if moved.Body.String() != target.Body.String() {
		t.Fatalf("migrating device body = %q; want the target sub's profiles %q", moved.Body.String(), target.Body.String())
	}
	if got, want := moved.Header().Get("Profile-Title"), target.Header().Get("Profile-Title"); got != want {
		t.Fatalf("Profile-Title = %q; want the target's %q", got, want)
	}
	if got := moved.Header().Get("new-url"); got == "" {
		t.Fatal("migrating device did not get new-url")
	}
	// Everyone else on the source sub still gets the source's profiles.
	if source.Header().Get("new-url") != "" {
		t.Fatal("unrelated device got new-url")
	}
}

func TestPublicSub_HistoryRecordsWhatWasSent(t *testing.T) {
	f := newMigrationFixture(t, nil)

	hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	hitSub(t, f.server, migrationTestSrcToken, "hwid-someone-else")

	rows, err := f.store.Queries.GetSubscriptionRequestHistory(context.Background(), store.GetSubscriptionRequestHistoryParams{
		Column1: 0, SubID: 0, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	var moved, plain int
	for _, row := range rows {
		switch {
		case row.ViaMigration == 1:
			moved++
			if row.SubID != f.dstID {
				t.Fatalf("migration request logged against sub %d; want target %d", row.SubID, f.dstID)
			}
			if row.MigratedFromName == "" {
				t.Fatal("migration request lost the name of the sub it was requested through")
			}
		case row.SubID == f.srcID:
			plain++
		}
	}
	if moved != 1 || plain != 1 {
		t.Fatalf("history rows: moved=%d plain=%d; want 1 and 1", moved, plain)
	}
}

func hitSubStatus(server *Server, token, hwid string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
	req.SetPathValue("token", token)
	req.Header.Set("User-Agent", "Happ/2.7.1")
	if hwid != "" {
		req.Header.Set("X-Hwid", hwid)
	}
	rec := httptest.NewRecorder()
	server.handlePublicSubscription(rec, req)
	return rec
}

func TestPublicSub_DeviceLimitRefusesNewDevicesOnly(t *testing.T) {
	server, st := newSubscriptionGoldenServer(t)
	ctx := context.Background()
	src, err := st.Queries.GetSubscriptionByToken(ctx, migrationTestSrcToken)
	if err != nil {
		t.Fatal(err)
	}

	for _, hwid := range []string{"device-a", "device-b"} {
		if rec := hitSubStatus(server, migrationTestSrcToken, hwid); rec.Code != http.StatusOK {
			t.Fatalf("%s before any limit status=%d", hwid, rec.Code)
		}
	}

	server.config.SubscriptionProtection.DefaultDeviceLimit = 2
	if rec := hitSubStatus(server, migrationTestSrcToken, "device-c"); rec.Code != http.StatusForbidden {
		t.Fatalf("third device under the default limit status=%d; want 403", rec.Code)
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, "device-a"); rec.Code != http.StatusOK {
		t.Fatalf("known device status=%d; want 200", rec.Code)
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("request without HWID status=%d; want 200", rec.Code)
	}

	if err := st.SetSubscriptionDeviceLimit(ctx, src.ID, 3); err != nil {
		t.Fatal(err)
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, "device-c"); rec.Code != http.StatusOK {
		t.Fatalf("third device under a per-sub limit of 3 status=%d; want 200", rec.Code)
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, "device-d"); rec.Code != http.StatusForbidden {
		t.Fatalf("fourth device status=%d; want 403", rec.Code)
	}

	rows, err := st.Queries.GetSubscriptionRequestHistory(ctx, store.GetSubscriptionRequestHistoryParams{Column1: 0, SubID: 0, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, row := range rows {
		if row.Blocked == 1 && row.BlockReason == "device_limit" {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("refused requests were not recorded with the device_limit reason")
	}
}

func TestPublicSub_DeviceLimitLetsAMovingDeviceIn(t *testing.T) {
	f := newMigrationFixture(t, nil)
	ctx := context.Background()
	// The target already holds one device and allows only one.
	hitSub(t, f.server, migrationTestDstToken, "resident-device")
	if err := f.store.SetSubscriptionDeviceLimit(ctx, f.dstID, 1); err != nil {
		t.Fatal(err)
	}

	if rec := hitSubStatus(f.server, migrationTestDstToken, "stranger-device"); rec.Code != http.StatusForbidden {
		t.Fatalf("stranger on a full sub status=%d; want 403", rec.Code)
	}
	if rec := hitSubStatus(f.server, migrationTestDstToken, migrationTestHWID); rec.Code != http.StatusOK {
		t.Fatalf("device being moved here status=%d; want 200", rec.Code)
	}
}

func TestPublicSub_DeviceLimitHoldsUnderConcurrentFirstRequests(t *testing.T) {
	server, _ := newSubscriptionGoldenServer(t)
	server.config.SubscriptionProtection.DefaultDeviceLimit = 3
	server.config.SubscriptionProtection.MaxRequests = 1000

	const devices = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	served := 0
	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := hitSubStatus(server, migrationTestSrcToken, fmt.Sprintf("concurrent-device-%d", i))
			if rec.Code == http.StatusOK {
				mu.Lock()
				served++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if served != 3 {
		t.Fatalf("%d devices were served under a limit of 3", served)
	}
}

func TestPublicSub_WorkerReportedBlockIsLoggedAndNeverServed(t *testing.T) {
	server, st := newSubscriptionGoldenServer(t)
	ctx := context.Background()

	send := func(token, reason, ua string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
		req.SetPathValue("token", token)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("X-CF-Client-IP", "203.0.113.50")
		req.Header.Set("X-OGS-Worker-Block", reason)
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		return rec
	}

	// Even a perfectly normal client User-Agent gets no content with the marker.
	rec := send(migrationTestSrcToken, "ua_browser", "Happ/2.7.1")
	if rec.Code != http.StatusForbidden || rec.Body.Len() > len("Forbidden\n") {
		t.Fatalf("status=%d body=%q; want a bare 403", rec.Code, rec.Body.String())
	}
	if rec := send(migrationTestSrcToken, "something-made-up", "Mozilla/5.0"); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown reason status=%d; want 403", rec.Code)
	}
	if rec := send("no-such-token", "ua_browser", "Mozilla/5.0"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token status=%d; want 404", rec.Code)
	}

	rows, err := st.Queries.GetSubscriptionRequestHistory(ctx, store.GetSubscriptionRequestHistoryParams{Column1: 0, SubID: 0, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]int{}
	for _, row := range rows {
		if row.Blocked == 1 {
			reasons[row.BlockReason]++
			if row.ViaWorker != 1 {
				t.Fatalf("a worker-reported block was not marked as coming through the worker: %+v", row)
			}
		}
	}
	if reasons["ua_browser"] != 1 {
		t.Fatalf("blocked reasons = %v; want one ua_browser", reasons)
	}
}

func TestPublicSub_TokenLookupCacheNeverOutlivesAChange(t *testing.T) {
	server, st := newSubscriptionGoldenServer(t)
	ctx := context.Background()
	sub, err := st.Queries.GetSubscriptionByToken(ctx, migrationTestSrcToken)
	if err != nil {
		t.Fatal(err)
	}

	// Warm the lookup cache, then regenerate the token through the handler.
	if rec := hitSubStatus(server, migrationTestSrcToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("first request status=%d", rec.Code)
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("cached request status=%d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/x/regenerate", nil)
	req.SetPathValue("id", fmt.Sprint(sub.ID))
	rec := httptest.NewRecorder()
	server.handleRegenerateSubscriptionToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := hitSubStatus(server, migrationTestSrcToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("old token after regenerate status=%d; want 404 at once", rec.Code)
	}

	// Unknown tokens are never cached as "found".
	for i := 0; i < 2; i++ {
		if rec := hitSubStatus(server, "never-existed", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("unknown token status=%d", rec.Code)
		}
	}
}
