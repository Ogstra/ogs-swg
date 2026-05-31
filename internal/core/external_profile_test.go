package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadExternalIPv6AcceptsLiteralOrFile(t *testing.T) {
	literal := "2800:40:80:685f:be24:11ff:fefa:cc41"
	if got := ReadExternalIPv6(literal); got != literal {
		t.Fatalf("literal IPv6=%q want %q", got, literal)
	}

	path := filepath.Join(t.TempDir(), "sing-box")
	if err := os.WriteFile(path, []byte(literal+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := ReadExternalIPv6(path); got != literal {
		t.Fatalf("file IPv6=%q want %q", got, literal)
	}
}

func TestUpsertExternalProfileCreatesMetadataUserAndAssignment(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.UpsertExternalProfile(ExternalProfile{
		Name:     "homelab-vless",
		Type:     "vless",
		HostIPv4: "example.test",
		Port:     443,
		UUID:     "11111111-1111-1111-1111-111111111111",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}

	meta, err := store.GetUserMetadata("homelab-vless")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil {
		t.Fatal("GetUserMetadata returned nil; want auto-created external user")
	}
	if len(meta.InboundTags) != 0 {
		t.Fatalf("InboundTags=%v; external users must not be assigned to local sing-box inbounds", meta.InboundTags)
	}

	profiles, err := store.GetUserExternalProfiles("homelab-vless")
	if err != nil {
		t.Fatalf("GetUserExternalProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != id {
		t.Fatalf("profiles=%#v; want one assignment to profile %d", profiles, id)
	}
}

func TestUpsertExternalProfileRenameMovesAssignmentWithoutLocalInbound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.UpsertExternalProfile(ExternalProfile{
		Name:    "old-homelab",
		Type:    "shadowsocks",
		Port:    8388,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO subscriptions (token, name) VALUES ('rename-token', 'Rename')`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO subscription_users (sub_id, user_name, alias, position) VALUES (1, 'old-homelab', 'Old Alias', 7)`); err != nil {
		t.Fatalf("insert subscription user: %v", err)
	}
	_, err = store.UpsertExternalProfile(ExternalProfile{
		ID:      id,
		Name:    "new-homelab",
		Type:    "shadowsocks",
		Port:    8388,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("rename profile: %v", err)
	}

	oldProfiles, err := store.GetUserExternalProfiles("old-homelab")
	if err != nil {
		t.Fatalf("GetUserExternalProfiles old: %v", err)
	}
	if len(oldProfiles) != 0 {
		t.Fatalf("old profile assignments=%#v; want none after rename", oldProfiles)
	}
	oldMeta, err := store.GetUserMetadata("old-homelab")
	if err != nil {
		t.Fatalf("GetUserMetadata old: %v", err)
	}
	if oldMeta != nil {
		t.Fatalf("old user metadata=%#v; want auto-created old user removed after rename", oldMeta)
	}
	newProfiles, err := store.GetUserExternalProfiles("new-homelab")
	if err != nil {
		t.Fatalf("GetUserExternalProfiles new: %v", err)
	}
	if len(newProfiles) != 1 || newProfiles[0].ID != id {
		t.Fatalf("new profile assignments=%#v; want renamed user assigned to profile %d", newProfiles, id)
	}

	meta, err := store.GetUserMetadata("new-homelab")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil || len(meta.InboundTags) != 0 {
		t.Fatalf("new user metadata=%#v; want metadata-only user without local inbounds", meta)
	}
	var oldSubUsers int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM subscription_users WHERE user_name = 'old-homelab'`).Scan(&oldSubUsers); err != nil {
		t.Fatalf("count old subscription users: %v", err)
	}
	if oldSubUsers != 0 {
		t.Fatalf("old subscription_users count=%d; want 0 after rename", oldSubUsers)
	}
	var alias string
	var position int
	if err := store.db.QueryRow(`SELECT alias, position FROM subscription_users WHERE sub_id = 1 AND user_name = 'new-homelab'`).Scan(&alias, &position); err != nil {
		t.Fatalf("new subscription membership missing after rename: %v", err)
	}
	if alias != "Old Alias" || position != 7 {
		t.Fatalf("renamed subscription membership alias=%q position=%d; want alias and position preserved", alias, position)
	}
}

func TestMigrateExistingSchemaRemovesOrphanSubscriptionUsers(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.db.Exec(`INSERT INTO subscriptions (token, name) VALUES ('orphan-token', 'Orphan')`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO subscription_users (sub_id, user_name, alias, position) VALUES (1, 'missing-external', '', 0)`); err != nil {
		t.Fatalf("insert orphan subscription user: %v", err)
	}

	if err := store.initSchema(); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM subscription_users WHERE user_name = 'missing-external'`).Scan(&count); err != nil {
		t.Fatalf("count orphan subscription users: %v", err)
	}
	if count != 0 {
		t.Fatalf("orphan subscription_users count=%d; want 0", count)
	}
}

func TestDeleteExternalProfileRemovesMetadataOnlyUserAndSubscriptionMembership(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.UpsertExternalProfile(ExternalProfile{
		Name:    "external-only",
		Type:    "vless",
		Port:    443,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO subscriptions (token, name) VALUES ('external-token', 'External')`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO subscription_users (sub_id, user_name, alias, position) VALUES (1, 'external-only', '', 0)`); err != nil {
		t.Fatalf("insert subscription user: %v", err)
	}

	if err := store.DeleteExternalProfile(id); err != nil {
		t.Fatalf("DeleteExternalProfile: %v", err)
	}
	meta, err := store.GetUserMetadata("external-only")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta != nil {
		t.Fatalf("metadata=%#v; want auto-created external user removed", meta)
	}
	var subUsers int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM subscription_users WHERE user_name = 'external-only'`).Scan(&subUsers); err != nil {
		t.Fatalf("count subscription_users: %v", err)
	}
	if subUsers != 0 {
		t.Fatalf("subscription_users count=%d; want 0", subUsers)
	}
}

func TestDeleteExternalProfilePreservesRealUser(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.SaveUserMetadata(UserMetadata{
		Email:       "real-user",
		Credential:  "11111111-1111-1111-1111-111111111111",
		QuotaPeriod: "monthly",
		Enabled:     true,
		InboundTags: []string{"vless-in"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}
	id, err := store.UpsertExternalProfile(ExternalProfile{
		Name:    "real-user",
		Type:    "vless",
		Port:    443,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}

	if err := store.DeleteExternalProfile(id); err != nil {
		t.Fatalf("DeleteExternalProfile: %v", err)
	}
	meta, err := store.GetUserMetadata("real-user")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if meta == nil {
		t.Fatal("real user metadata was removed")
	}
	if meta.Credential == "" || len(meta.InboundTags) != 1 || meta.InboundTags[0] != "vless-in" {
		t.Fatalf("real user metadata=%#v; want preserved sing-box identity", meta)
	}
}
