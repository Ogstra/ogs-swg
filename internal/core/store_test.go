package core

import (
	"path/filepath"
	"testing"
	"time"
)

func sumSamples(samples []Sample) (int64, int64) {
	var uplink int64
	var downlink int64
	for _, sample := range samples {
		uplink += sample.Uplink
		downlink += sample.Downlink
	}
	return uplink, downlink
}

func TestRenameUserTrafficIdentity_MigratesSamplesAndDailyUsage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	oldRawTs := now - 72*3600
	recentRawTs := now - 1800

	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: oldRawTs, Uplink: 120, Downlink: 240},
		{User: "alice", Timestamp: recentRawTs, Uplink: 30, Downlink: 60},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	if err := store.CompressOldSamples(now - 24*3600); err != nil {
		t.Fatalf("CompressOldSamples: %v", err)
	}

	if err := store.RenameUserTrafficIdentity("alice", "alice-renamed"); err != nil {
		t.Fatalf("RenameUserTrafficIdentity: %v", err)
	}

	recent, err := store.GetSamples("alice-renamed", now-7*24*3600, now)
	if err != nil {
		t.Fatalf("GetSamples(new): %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("GetSamples(new) returned %d rows; want 1 recent raw sample", len(recent))
	}
	if recent[0].User != "alice-renamed" {
		t.Fatalf("GetSamples(new) user = %q; want %q", recent[0].User, "alice-renamed")
	}

	combined, err := store.GetCombinedReport("alice-renamed", now-7*24*3600, now)
	if err != nil {
		t.Fatalf("GetCombinedReport(new): %v", err)
	}
	if len(combined) != 2 {
		t.Fatalf("GetCombinedReport(new) returned %d rows; want raw + compressed history", len(combined))
	}

	up, down := sumSamples(combined)
	if up != 150 || down != 300 {
		t.Fatalf("GetCombinedReport(new) totals = (%d, %d); want (150, 300)", up, down)
	}

	oldRecent, err := store.GetSamples("alice", now-7*24*3600, now)
	if err != nil {
		t.Fatalf("GetSamples(old): %v", err)
	}
	if len(oldRecent) != 0 {
		t.Fatalf("GetSamples(old) returned %d rows; want 0 after migration", len(oldRecent))
	}

	oldCombined, err := store.GetCombinedReport("alice", now-7*24*3600, now)
	if err != nil {
		t.Fatalf("GetCombinedReport(old): %v", err)
	}
	if len(oldCombined) != 0 {
		t.Fatalf("GetCombinedReport(old) returned %d rows; want 0 after migration", len(oldCombined))
	}
}
