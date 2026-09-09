package core

import (
	"context"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// ntfy_settings store tests
// ---------------------------------------------------------------------------

func TestNtfySettingsStore_FreshDBDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}
	if got.AuthMode != "none" {
		t.Errorf("AuthMode = %q, want %q on fresh DB", got.AuthMode, "none")
	}
	if got.ServerURL != "" || got.Topic != "" {
		t.Errorf("expected empty ServerURL/Topic on fresh DB, got %+v", got)
	}
}

func TestNtfySettingsStore_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	in := NtfySettings{
		ServerURL:             "https://ntfy.example.internal",
		Topic:                 "panel-alerts",
		AuthMode:              "bearer",
		BearerToken:           "test-token-placeholder",
		EnableSingboxDown:     true,
		EnableWireguardDown:   true,
		EnableHighTraffic:     true,
		EnableConfigErrors:    true,
		TrafficThresholdBytes: 123456789,
	}

	if err := store.UpdateNtfySettings(context.Background(), in); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}

	got, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}

	if got.ServerURL != in.ServerURL {
		t.Errorf("ServerURL = %q, want %q", got.ServerURL, in.ServerURL)
	}
	if got.Topic != in.Topic {
		t.Errorf("Topic = %q, want %q", got.Topic, in.Topic)
	}
	if got.AuthMode != in.AuthMode {
		t.Errorf("AuthMode = %q, want %q", got.AuthMode, in.AuthMode)
	}
	if got.BearerToken != in.BearerToken {
		t.Errorf("BearerToken = %q, want %q", got.BearerToken, in.BearerToken)
	}
	if got.EnableSingboxDown != in.EnableSingboxDown {
		t.Errorf("EnableSingboxDown = %v, want %v", got.EnableSingboxDown, in.EnableSingboxDown)
	}
	if got.EnableWireguardDown != in.EnableWireguardDown {
		t.Errorf("EnableWireguardDown = %v, want %v", got.EnableWireguardDown, in.EnableWireguardDown)
	}
	if got.EnableHighTraffic != in.EnableHighTraffic {
		t.Errorf("EnableHighTraffic = %v, want %v", got.EnableHighTraffic, in.EnableHighTraffic)
	}
	if got.EnableConfigErrors != in.EnableConfigErrors {
		t.Errorf("EnableConfigErrors = %v, want %v", got.EnableConfigErrors, in.EnableConfigErrors)
	}
	if got.TrafficThresholdBytes != in.TrafficThresholdBytes {
		t.Errorf("TrafficThresholdBytes = %d, want %d", got.TrafficThresholdBytes, in.TrafficThresholdBytes)
	}
}

func TestNtfySettingsStore_UpdateTwiceLeavesOneRow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	first := NtfySettings{ServerURL: "https://first.example.internal", Topic: "first-topic", AuthMode: "none"}
	second := NtfySettings{ServerURL: "https://second.example.internal", Topic: "second-topic", AuthMode: "none"}

	if err := store.UpdateNtfySettings(context.Background(), first); err != nil {
		t.Fatalf("UpdateNtfySettings(first): %v", err)
	}
	if err := store.UpdateNtfySettings(context.Background(), second); err != nil {
		t.Fatalf("UpdateNtfySettings(second): %v", err)
	}

	var rowCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM ntfy_settings").Scan(&rowCount); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("row count = %d, want 1", rowCount)
	}

	got, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}
	if got.ServerURL != second.ServerURL {
		t.Errorf("ServerURL = %q, want second call's value %q", got.ServerURL, second.ServerURL)
	}
}

func TestNtfySettingsStore_PersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	in := NtfySettings{
		ServerURL: "https://reopen.example.internal",
		Topic:     "reopen-topic",
		AuthMode:  "basic",
		BasicUser: "testuser",
		BasicPass: "test-pass-placeholder",
	}
	if err := store.UpdateNtfySettings(context.Background(), in); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (reopen): %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	got, err := reopened.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings (reopen): %v", err)
	}
	if got.ServerURL != in.ServerURL || got.Topic != in.Topic {
		t.Errorf("reopened settings = %+v, want ServerURL=%q Topic=%q", got, in.ServerURL, in.Topic)
	}
	if got.BasicUser != in.BasicUser || got.BasicPass != in.BasicPass {
		t.Errorf("reopened basic auth = %+v, want user=%q pass=%q", got, in.BasicUser, in.BasicPass)
	}
}
