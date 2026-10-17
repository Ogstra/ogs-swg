package core

import (
	"context"
	"path/filepath"
	"testing"
)

func newMigrationTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, tok := range []string{"src-token", "dst-token"} {
		if _, err := store.db.Exec(`INSERT INTO subscriptions (token, name) VALUES (?, ?)`, tok, tok); err != nil {
			t.Fatalf("seed subscription: %v", err)
		}
	}
	return store
}

func TestHWIDMigration_LifecycleDeliveredThenConfirmed(t *testing.T) {
	ctx := context.Background()
	store := newMigrationTestStore(t)

	if err := store.CreateHWIDMigration(ctx, 1, "hash-a", 2, 100); err != nil {
		t.Fatalf("CreateHWIDMigration: %v", err)
	}

	m, err := store.GetLiveHWIDMigration(ctx, 1, "hash-a")
	if err != nil || m == nil || m.TargetSubID != 2 || m.Status != "pending" {
		t.Fatalf("live migration = %#v, err=%v; want pending -> sub 2", m, err)
	}

	if _, err := store.MarkHWIDMigrationDelivered(ctx, 1, "hash-a", 200); err != nil {
		t.Fatalf("MarkHWIDMigrationDelivered: %v", err)
	}
	m, _ = store.GetLiveHWIDMigration(ctx, 1, "hash-a")
	if m == nil || m.Status != "delivered" || m.Attempts != 1 {
		t.Fatalf("after delivery = %#v; want delivered, attempts=1", m)
	}

	confirmed, err := store.ConfirmHWIDMigration(ctx, 2, "hash-a", 300)
	if err != nil || !confirmed {
		t.Fatalf("ConfirmHWIDMigration = %v, %v; want true", confirmed, err)
	}
	again, _ := store.ConfirmHWIDMigration(ctx, 2, "hash-a", 400)
	if again {
		t.Fatalf("second ConfirmHWIDMigration = true; confirmation must fire once")
	}
	if live, _ := store.GetLiveHWIDMigration(ctx, 1, "hash-a"); live != nil {
		t.Fatalf("confirmed migration still live: %#v", live)
	}
}

func TestHWIDMigration_FailsAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	store := newMigrationTestStore(t)
	if err := store.CreateHWIDMigration(ctx, 1, "hash-a", 2, 100); err != nil {
		t.Fatal(err)
	}
	now := int64(200)
	var failedNow bool
	for i := 0; i < MaxHWIDMigrationAttempts; i++ {
		var err error
		failedNow, err = store.MarkHWIDMigrationDelivered(ctx, 1, "hash-a", now)
		if err != nil {
			t.Fatal(err)
		}
		now += HWIDMigrationAttemptGapSeconds
	}
	if !failedNow {
		t.Fatalf("last counted attempt did not report the failure flip")
	}
	if live, _ := store.GetLiveHWIDMigration(ctx, 1, "hash-a"); live != nil {
		t.Fatalf("migration still live after %d attempts: %#v", MaxHWIDMigrationAttempts, live)
	}
}

func TestHWIDMigration_RapidRepeatsCountOnce(t *testing.T) {
	ctx := context.Background()
	store := newMigrationTestStore(t)
	if err := store.CreateHWIDMigration(ctx, 1, "hash-a", 2, 100); err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 20; i++ {
		if _, err := store.MarkHWIDMigrationDelivered(ctx, 1, "hash-a", 200+i); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := store.GetLiveHWIDMigration(ctx, 1, "hash-a")
	if m == nil || m.Attempts != 1 {
		t.Fatalf("rapid repeats = %#v; want still live with attempts=1", m)
	}
}

func TestHWIDMigration_CancelAll(t *testing.T) {
	ctx := context.Background()
	store := newMigrationTestStore(t)
	_ = store.CreateHWIDMigration(ctx, 1, "hash-a", 2, 100)
	_ = store.CreateHWIDMigration(ctx, 1, "hash-b", 2, 100)
	n, err := store.CancelAllHWIDMigrations(ctx)
	if err != nil || n != 2 {
		t.Fatalf("CancelAllHWIDMigrations = %d, %v; want 2", n, err)
	}
	if live, _ := store.GetLiveHWIDMigration(ctx, 1, "hash-a"); live != nil {
		t.Fatalf("migration still live after cancel-all")
	}
}

func TestHWIDMigration_RejectsSelfTargetAndEmptyHash(t *testing.T) {
	ctx := context.Background()
	store := newMigrationTestStore(t)
	if err := store.CreateHWIDMigration(ctx, 1, "hash-a", 1, 100); err == nil {
		t.Fatalf("self-target migration accepted")
	}
	if err := store.CreateHWIDMigration(ctx, 1, "  ", 2, 100); err == nil {
		t.Fatalf("empty hwid accepted")
	}
}
