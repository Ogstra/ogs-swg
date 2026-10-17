package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
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
