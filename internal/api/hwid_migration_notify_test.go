package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// recordMigrationNtfy enables the new-device toggle and captures every ntfy
// message the server publishes.
func recordMigrationNtfy(t *testing.T, f *migrationFixture) func() []core.NtfyMessage {
	t.Helper()
	if err := f.store.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL:     "https://ntfy.example.test",
		Topic:         "test-topic",
		EnableNewHwid: true,
	}); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}
	var mu sync.Mutex
	var published []core.NtfyMessage
	f.server.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return f.store.GetNtfySettings(ctx) },
		func(_ context.Context, _ core.NtfySettings, m core.NtfyMessage) error {
			mu.Lock()
			defer mu.Unlock()
			published = append(published, m)
			return nil
		},
	)
	return func() []core.NtfyMessage {
		mu.Lock()
		defer mu.Unlock()
		return append([]core.NtfyMessage(nil), published...)
	}
}

func TestPublicSub_MigrationFailureNotifiesOnce(t *testing.T) {
	f := newMigrationFixture(t, nil)
	snapshot := recordMigrationNtfy(t, f)

	now := f.server.now()
	for i := 0; i < core.MaxHWIDMigrationAttempts+2; i++ {
		at := now.Add(time.Duration(i*core.HWIDMigrationAttemptGapSeconds) * time.Second)
		f.server.now = func() time.Time { return at }
		hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	}

	var failures int
	for _, m := range snapshot() {
		if strings.HasPrefix(m.Title, "Device move failed:") {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("failure notifications = %d; want exactly 1", failures)
	}
	if live, _ := f.store.GetLiveHWIDMigration(context.Background(), f.srcID, f.hash); live != nil {
		t.Fatalf("migration still live after max attempts: %#v", live)
	}
}
