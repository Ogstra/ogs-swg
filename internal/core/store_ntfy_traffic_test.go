package core

import (
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// GetCombinedTrafficTotal (NTFY-02 support)
// ---------------------------------------------------------------------------

func insertWGSample(t *testing.T, store *Store, publicKey string, ts, rx, tx int64) {
	t.Helper()
	if _, err := store.db.Exec(
		`INSERT INTO wg_samples (public_key, ts, rx, tx, endpoint) VALUES (?, ?, ?, ?, '')`,
		publicKey, ts, rx, tx,
	); err != nil {
		t.Fatalf("insertWGSample: %v", err)
	}
}

func TestGetCombinedTrafficTotal_EmptyReturnsZero(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	total, err := store.GetCombinedTrafficTotal(0, 1000)
	if err != nil {
		t.Fatalf("GetCombinedTrafficTotal: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
}

func TestGetCombinedTrafficTotal_SingboxOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: 100, Uplink: 50, Downlink: 25},
		{User: "alice", Timestamp: 200, Uplink: 10, Downlink: 5},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	total, err := store.GetCombinedTrafficTotal(0, 1000)
	if err != nil {
		t.Fatalf("GetCombinedTrafficTotal: %v", err)
	}
	if total != 90 {
		t.Fatalf("total = %d; want 90", total)
	}
}

func TestGetCombinedTrafficTotal_WireGuardOnly_CounterResetSafe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// First sample seeds the counters (no prior row -> delta 0).
	insertWGSample(t, store, "peer-a", 100, 1000, 2000)
	// Normal increase: +500 rx, +300 tx.
	insertWGSample(t, store, "peer-a", 200, 1500, 2300)
	// Counter reset (interface restart): rx/tx drop below prior sample -> delta must be 0, not negative.
	insertWGSample(t, store, "peer-a", 300, 100, 50)

	total, err := store.GetCombinedTrafficTotal(0, 1000)
	if err != nil {
		t.Fatalf("GetCombinedTrafficTotal: %v", err)
	}
	if total != 800 {
		t.Fatalf("total = %d; want 800 (500 rx + 300 tx, reset contributes 0)", total)
	}
}

func TestGetCombinedTrafficTotal_RowsOutsideRangeExcluded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: 50, Uplink: 999, Downlink: 999}, // out of range
		{User: "alice", Timestamp: 150, Uplink: 10, Downlink: 5},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	insertWGSample(t, store, "peer-a", 50, 1000, 1000) // out of range, seeds nothing in range
	insertWGSample(t, store, "peer-a", 150, 1100, 1100)

	total, err := store.GetCombinedTrafficTotal(100, 200)
	if err != nil {
		t.Fatalf("GetCombinedTrafficTotal: %v", err)
	}
	if total != 15 {
		t.Fatalf("total = %d; want 15 (only the in-range sing-box row; wg row has no in-range predecessor)", total)
	}
}

func TestGetCombinedTrafficTotal_BothTablesSummed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: 100, Uplink: 50, Downlink: 25},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	insertWGSample(t, store, "peer-a", 100, 1000, 2000)
	insertWGSample(t, store, "peer-a", 200, 1500, 2300)

	total, err := store.GetCombinedTrafficTotal(0, 1000)
	if err != nil {
		t.Fatalf("GetCombinedTrafficTotal: %v", err)
	}
	// sing-box: 75, wireguard: 500 rx + 300 tx = 800
	if total != 875 {
		t.Fatalf("total = %d; want 875", total)
	}
}
