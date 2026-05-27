package core

import (
	"path/filepath"
	"testing"
)

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
}
