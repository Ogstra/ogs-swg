package core

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// PanelUser permission tests — can_read_logs_censored
// ---------------------------------------------------------------------------

func TestPanelUserCanReadLogsCensored_CreateAndVerify(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	perms := PanelUserPermissions{
		CanReadLogs:         false,
		CanReadLogsCensored: true,
	}
	if err := store.CreatePanelUser("bob", "secret", perms); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	got, err := store.VerifyPanelUser("bob", "secret")
	if err != nil {
		t.Fatalf("VerifyPanelUser: %v", err)
	}
	if got == nil {
		t.Fatal("VerifyPanelUser returned nil; want permissions struct")
	}
	if !got.CanReadLogsCensored {
		t.Errorf("CanReadLogsCensored = false after Create; want true")
	}
	// Normalize() must have implied CanReadLogs=true
	if !got.CanReadLogs {
		t.Errorf("CanReadLogs = false after Normalize; want true (implied by CanReadLogsCensored)")
	}
}

func TestPanelUserCanReadLogsCensored_UpdateAndGetAll(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create with censored=false
	if err := store.CreatePanelUser("carol", "pass", PanelUserPermissions{}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	// Update with censored=true
	if err := store.UpdatePanelUserPermissions("carol", PanelUserPermissions{CanReadLogsCensored: true}); err != nil {
		t.Fatalf("UpdatePanelUserPermissions: %v", err)
	}

	users, err := store.GetAllPanelUsers()
	if err != nil {
		t.Fatalf("GetAllPanelUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("GetAllPanelUsers returned %d users; want 1", len(users))
	}
	if !users[0].Permissions.CanReadLogsCensored {
		t.Errorf("CanReadLogsCensored = false after Update; want true")
	}
	if !users[0].Permissions.CanReadLogs {
		t.Errorf("CanReadLogs = false after Normalize; want true (implied by CanReadLogsCensored)")
	}
}

func TestPanelUserCanReadLogsCensored_DefaultFalse(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create without CanReadLogsCensored
	if err := store.CreatePanelUser("dave", "pw", PanelUserPermissions{CanReadLogs: true}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	got, err := store.VerifyPanelUser("dave", "pw")
	if err != nil {
		t.Fatalf("VerifyPanelUser: %v", err)
	}
	if got == nil {
		t.Fatal("VerifyPanelUser returned nil")
	}
	if got.CanReadLogsCensored {
		t.Errorf("CanReadLogsCensored = true; want false (was not set)")
	}
}

func TestPanelUserNormalize_CensoredImpliesRead(t *testing.T) {
	p := PanelUserPermissions{CanReadLogsCensored: true}
	p.Normalize()
	if !p.CanReadLogs {
		t.Errorf("Normalize(): CanReadLogs = false; want true when CanReadLogsCensored=true")
	}
}

func TestFullPanelUserPermissions_IncludesCensored(t *testing.T) {
	p := fullPanelUserPermissions()
	if !p.CanReadLogsCensored {
		t.Errorf("fullPanelUserPermissions().CanReadLogsCensored = false; want true")
	}
}

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
	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "alice",
		QuotaLimit:  1024,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
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

	newMeta, err := store.GetUserMetadata("alice-renamed")
	if err != nil {
		t.Fatalf("GetUserMetadata(new): %v", err)
	}
	if newMeta == nil || newMeta.Email != "alice-renamed" {
		t.Fatalf("GetUserMetadata(new) = %#v; want renamed metadata row", newMeta)
	}
	if len(newMeta.InboundTags) != 1 || newMeta.InboundTags[0] != "test-vless" {
		t.Fatalf("GetUserMetadata(new).InboundTags = %#v; want [test-vless]", newMeta.InboundTags)
	}

	oldMeta, err := store.GetUserMetadata("alice")
	if err != nil {
		t.Fatalf("GetUserMetadata(old): %v", err)
	}
	if oldMeta != nil {
		t.Fatalf("GetUserMetadata(old) = %#v; want nil after migration", oldMeta)
	}
}

func TestGetSBTopTotals_IncludesCompressedHistory(t *testing.T) {
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
	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "alice",
		QuotaLimit:  1024,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	if err := store.CompressOldSamples(now - 24*3600); err != nil {
		t.Fatalf("CompressOldSamples: %v", err)
	}
	if err := store.RenameUserTrafficIdentity("alice", "alice-renamed"); err != nil {
		t.Fatalf("RenameUserTrafficIdentity: %v", err)
	}

	totals, err := store.GetSBTopTotals(1, now, 10)
	if err != nil {
		t.Fatalf("GetSBTopTotals: %v", err)
	}
	if len(totals) != 1 {
		t.Fatalf("GetSBTopTotals returned %d rows; want 1 renamed identity", len(totals))
	}
	if totals[0].Key != "alice-renamed" {
		t.Fatalf("GetSBTopTotals key = %q; want %q", totals[0].Key, "alice-renamed")
	}
	if totals[0].Total != 450 {
		t.Fatalf("GetSBTopTotals total = %d; want %d", totals[0].Total, 450)
	}
	if totals[0].Rx != 300 || totals[0].Tx != 150 {
		t.Fatalf("GetSBTopTotals rx/tx = (%d, %d); want (300, 150)", totals[0].Rx, totals[0].Tx)
	}
}

func TestNewStore_MigratesLegacySubscriptionRequestsBeforeCreatingBlockedIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-store.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	legacySchema := `
	CREATE TABLE subscription_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sub_id INTEGER NOT NULL,
		requested_at INTEGER NOT NULL,
		served_from_cache INTEGER NOT NULL DEFAULT 0
	);
	`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("creating legacy schema: %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore should migrate legacy subscription_requests schema: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var blocked sql.NullInt64
	if err := store.db.QueryRow("SELECT blocked FROM subscription_requests LIMIT 1").Scan(&blocked); err != nil && err != sql.ErrNoRows {
		t.Fatalf("blocked column missing after migration: %v", err)
	}

	var indexName string
	if err := store.db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_subscription_requests_blocked'").Scan(&indexName); err != nil {
		t.Fatalf("blocked index missing after migration: %v", err)
	}
}

func TestEnforceUserQuotas_DisablesExceededUserAndRemovesFromConfig(t *testing.T) {
	cfg, _ := newTestConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}]
			}
		]
	}`)

	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: now - 60, Uplink: 80, Downlink: 40},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "alice",
		QuotaLimit:  100,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	store.EnforceUserQuotas(cfg)

	meta, err := store.GetUserMetadata("alice")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil || meta.Enabled {
		t.Fatalf("alice metadata = %#v; want disabled metadata", meta)
	}

	users, err := cfg.GetActiveUsers()
	if err != nil {
		t.Fatalf("GetActiveUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("active users = %#v; want alice removed from config", users)
	}
}

func TestEnforceUserQuotas_ReEnablesUserWhenUnderLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg, _ := newTestConfig(t, `{"inbounds":[]}`)

	now := time.Now().Unix()
	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: now - 60, Uplink: 20, Downlink: 30},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "alice",
		QuotaLimit:  100,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     false,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	store.EnforceUserQuotas(cfg)

	meta, err := store.GetUserMetadata("alice")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil || !meta.Enabled {
		t.Fatalf("alice metadata = %#v; want re-enabled metadata", meta)
	}
}

func TestEnforceSubscriptionQuotas_DoesNotReenableUserStillOverOwnQuota(t *testing.T) {
	cfg, _ := newTestConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": []
			}
		]
	}`)

	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	if err := store.BulkInsert([]Sample{
		{User: "alice", Timestamp: now - 60, Uplink: 120, Downlink: 90},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "alice",
		QuotaLimit:  100,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     false,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	subID, err := store.Queries.CreateSubscription(context.Background(), sqlcStore.CreateSubscriptionParams{
		Token:       "sub-token",
		Name:        "bundle",
		QuotaLimit:  sql.NullInt64{Int64: 1000, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := store.Queries.AddUserToSubscription(context.Background(), sqlcStore.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	store.EnforceSubscriptionQuotas(cfg)

	meta, err := store.GetUserMetadata("alice")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil || meta.Enabled {
		t.Fatalf("alice metadata = %#v; want still disabled because own quota is exceeded", meta)
	}
}
