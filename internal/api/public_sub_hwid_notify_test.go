package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// newHWIDNotifyTestSubscription creates a subscription with a single member
// "alice" and returns its ID.
func newHWIDNotifyTestSubscription(t *testing.T, dataStore *core.Store, token, name string) int64 {
	t.Helper()

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       token,
		Name:        name,
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
	return subID
}

func TestPublicSubscriptionNewHWIDNotification(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.now = func() time.Time { return time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) }
	server.config.SubscriptionProtection = core.SubscriptionProtectionConfig{}

	tokenA := "hwid-notify-token-a"
	tokenB := "hwid-notify-token-b"
	newHWIDNotifyTestSubscription(t, dataStore, tokenA, "Alpha Bundle")
	subBID := newHWIDNotifyTestSubscription(t, dataStore, tokenB, "Beta Bundle")

	if err := dataStore.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL:     "https://ntfy.example.com",
		Topic:         "test-topic",
		EnableNewHwid: true,
	}); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}

	var mu sync.Mutex
	var published []core.NtfyMessage
	server.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return dataStore.GetNtfySettings(ctx) },
		func(_ context.Context, _ core.NtfySettings, m core.NtfyMessage) error {
			mu.Lock()
			defer mu.Unlock()
			published = append(published, m)
			return nil
		},
	)

	snapshotPublished := func() []core.NtfyMessage {
		mu.Lock()
		defer mu.Unlock()
		out := make([]core.NtfyMessage, len(published))
		copy(out, published)
		return out
	}

	makeRequest := func(token string, configure func(r *http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
		req.SetPathValue("token", token)
		if configure != nil {
			configure(req)
		}
		rec := httptest.NewRecorder()
		server.handlePublicSubscription(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request token=%s status=%d body=%q", token, rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("request token=%s: empty body", token)
		}
		return rec
	}

	// Test 1: first request with a new HWID publishes exactly 1 message with
	// expected content.
	makeRequest(tokenA, func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-aaa")
		r.Header.Set("X-Device-Model", "iPhone15,2")
		r.Header.Set("X-Device-OS", "iOS")
		r.Header.Set("X-Ver-Os", "17.4")
		r.Header.Set("X-App-Version", "2.7.1")
		r.Header.Set("CF-IPCountry", "AR")
	})
	got := snapshotPublished()
	if len(got) != 1 {
		t.Fatalf("after first new-hwid request: len(published)=%d want 1", len(got))
	}
	if !strings.HasPrefix(got[0].Title, "New device on subscription:") {
		t.Errorf("Title=%q want prefix %q", got[0].Title, "New device on subscription:")
	}
	for _, want := range []string{"iPhone 14 Pro", "iOS", "17.4", "2.7.1", "AR"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("Message=%q missing %q", got[0].Message, want)
		}
	}

	// Test 2: 4 more identical requests (including cache-hit path) publish 0
	// further messages.
	for i := 0; i < 4; i++ {
		makeRequest(tokenA, func(r *http.Request) {
			r.Header.Set("X-Hwid", "hwid-device-aaa")
		})
	}
	if got := snapshotPublished(); len(got) != 1 {
		t.Fatalf("after 4 repeat requests: len(published)=%d want 1", len(got))
	}

	// Test 3: a different HWID on the same subscription publishes 1 more.
	makeRequest(tokenA, func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-bbb")
	})
	if got := snapshotPublished(); len(got) != 2 {
		t.Fatalf("after second device on same sub: len(published)=%d want 2", len(got))
	}

	// Test 4: same HWID against a different subscription publishes again
	// (per-subscription scoping).
	makeRequest(tokenB, func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-aaa")
	})
	if got := snapshotPublished(); len(got) != 3 {
		t.Fatalf("after same hwid on different sub: len(published)=%d want 3", len(got))
	}

	// Test 5: a request with no X-Hwid header publishes 0 and leaves known
	// HWID count unchanged.
	countBefore, err := dataStore.CountSubscriptionKnownHWIDs(context.Background(), subBID)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
	}
	makeRequest(tokenB, nil)
	if got := snapshotPublished(); len(got) != 3 {
		t.Fatalf("after no-hwid request: len(published)=%d want 3", len(got))
	}
	countAfter, err := dataStore.CountSubscriptionKnownHWIDs(context.Background(), subBID)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
	}
	if countAfter != countBefore {
		t.Errorf("CountSubscriptionKnownHWIDs changed on no-hwid request: before=%d after=%d", countBefore, countAfter)
	}

	// Test 6: with EnableNewHwid disabled, a brand-new HWID publishes nothing
	// but is still recorded in the store.
	if err := dataStore.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL:     "https://ntfy.example.com",
		Topic:         "test-topic",
		EnableNewHwid: false,
	}); err != nil {
		t.Fatalf("UpdateNtfySettings (disable): %v", err)
	}
	countBefore, err = dataStore.CountSubscriptionKnownHWIDs(context.Background(), subBID)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
	}
	makeRequest(tokenB, func(r *http.Request) {
		r.Header.Set("X-Hwid", "hwid-device-ccc")
	})
	if got := snapshotPublished(); len(got) != 3 {
		t.Fatalf("after disabled-toggle new-hwid request: len(published)=%d want 3", len(got))
	}
	countAfter, err = dataStore.CountSubscriptionKnownHWIDs(context.Background(), subBID)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
	}
	if countAfter != countBefore+1 {
		t.Errorf("CountSubscriptionKnownHWIDs after disabled-toggle request: before=%d after=%d want +1", countBefore, countAfter)
	}

	// Re-enable for the concurrency test.
	if err := dataStore.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL:     "https://ntfy.example.com",
		Topic:         "test-topic",
		EnableNewHwid: true,
	}); err != nil {
		t.Fatalf("UpdateNtfySettings (re-enable): %v", err)
	}

	// Test 7: 10 concurrent requests with the same brand-new HWID publish
	// exactly 1 message total.
	beforeConcurrent := len(snapshotPublished())
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/s/"+tokenA, nil)
			req.SetPathValue("token", tokenA)
			req.Header.Set("X-Hwid", "hwid-device-concurrent")
			rec := httptest.NewRecorder()
			server.handlePublicSubscription(rec, req)
			if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
				t.Errorf("concurrent request status=%d bodyLen=%d", rec.Code, rec.Body.Len())
			}
		}()
	}
	wg.Wait()
	afterConcurrent := len(snapshotPublished())
	if afterConcurrent-beforeConcurrent != 1 {
		t.Fatalf("concurrent new-hwid requests: published delta=%d want 1", afterConcurrent-beforeConcurrent)
	}
}
