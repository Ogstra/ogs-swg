package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// subscription_known_hwids store tests
// ---------------------------------------------------------------------------

func newHWIDTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRecordSubscriptionHWIDFirstSeen(t *testing.T) {
	t.Run("first call reports new", func(t *testing.T) {
		store := newHWIDTestStore(t)
		isNew, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, "hashaaa", 1000)
		if err != nil {
			t.Fatalf("RecordSubscriptionHWIDFirstSeen: %v", err)
		}
		if !isNew {
			t.Errorf("isNew = false, want true on first call")
		}
	})

	t.Run("repeat call reports not-new and keeps first_seen_at", func(t *testing.T) {
		store := newHWIDTestStore(t)
		if _, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, "hashaaa", 1000); err != nil {
			t.Fatalf("RecordSubscriptionHWIDFirstSeen (first): %v", err)
		}
		isNew, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, "hashaaa", 2000)
		if err != nil {
			t.Fatalf("RecordSubscriptionHWIDFirstSeen (second): %v", err)
		}
		if isNew {
			t.Errorf("isNew = true, want false on repeat call")
		}

		var firstSeenAt int64
		if err := store.db.QueryRow(
			`SELECT first_seen_at FROM subscription_known_hwids WHERE sub_id = ? AND hwid_hash = ?`, 1, "hashaaa",
		).Scan(&firstSeenAt); err != nil {
			t.Fatalf("query first_seen_at: %v", err)
		}
		if firstSeenAt != 1000 {
			t.Errorf("first_seen_at = %d, want 1000 (must not be overwritten)", firstSeenAt)
		}
	})

	t.Run("same hash under different sub reports new", func(t *testing.T) {
		store := newHWIDTestStore(t)
		if _, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, "hashaaa", 1000); err != nil {
			t.Fatalf("RecordSubscriptionHWIDFirstSeen (sub 1): %v", err)
		}
		isNew, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 2, "hashaaa", 1000)
		if err != nil {
			t.Fatalf("RecordSubscriptionHWIDFirstSeen (sub 2): %v", err)
		}
		if !isNew {
			t.Errorf("isNew = false, want true for same hash under a different sub_id")
		}
	})

	t.Run("empty or whitespace hash is ignored", func(t *testing.T) {
		store := newHWIDTestStore(t)
		for _, hash := range []string{"", "   "} {
			isNew, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, hash, 1000)
			if err != nil {
				t.Fatalf("RecordSubscriptionHWIDFirstSeen(%q): %v", hash, err)
			}
			if isNew {
				t.Errorf("RecordSubscriptionHWIDFirstSeen(%q) isNew = true, want false", hash)
			}
		}

		count, err := store.CountSubscriptionKnownHWIDs(context.Background(), 1)
		if err != nil {
			t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0 (empty/whitespace hash must not be stored)", count)
		}
	})

	t.Run("concurrent identical recordings yield exactly one new", func(t *testing.T) {
		store := newHWIDTestStore(t)
		const goroutines = 20
		results := make(chan bool, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				isNew, err := store.RecordSubscriptionHWIDFirstSeen(context.Background(), 1, "hashbbb", 1000)
				if err != nil {
					t.Errorf("RecordSubscriptionHWIDFirstSeen: %v", err)
					return
				}
				results <- isNew
			}()
		}
		wg.Wait()
		close(results)

		newCount := 0
		for isNew := range results {
			if isNew {
				newCount++
			}
		}
		if newCount != 1 {
			t.Errorf("newCount = %d, want exactly 1 across %d concurrent calls", newCount, goroutines)
		}
	})
}

func TestCountSubscriptionKnownHWIDs(t *testing.T) {
	store := newHWIDTestStore(t)
	ctx := context.Background()

	count, err := store.CountSubscriptionKnownHWIDs(ctx, 1)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs (empty): %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for a subscription with no recorded HWIDs", count)
	}

	if _, err := store.RecordSubscriptionHWIDFirstSeen(ctx, 1, "hashaaa", 1000); err != nil {
		t.Fatalf("RecordSubscriptionHWIDFirstSeen: %v", err)
	}
	if _, err := store.RecordSubscriptionHWIDFirstSeen(ctx, 1, "hashbbb", 1000); err != nil {
		t.Fatalf("RecordSubscriptionHWIDFirstSeen: %v", err)
	}
	// Recording sub 2's hash must not affect sub 1's count.
	if _, err := store.RecordSubscriptionHWIDFirstSeen(ctx, 2, "hashccc", 1000); err != nil {
		t.Fatalf("RecordSubscriptionHWIDFirstSeen: %v", err)
	}

	count, err = store.CountSubscriptionKnownHWIDs(ctx, 1)
	if err != nil {
		t.Fatalf("CountSubscriptionKnownHWIDs: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}
