package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// ntfyPollExecutorStub is a controllable SystemExecutor stub whose
// IsServiceActive result can be flipped per service between calls.
type ntfyPollExecutorStub struct {
	mu     sync.Mutex
	active map[string]bool
}

func newNtfyPollExecutorStub() *ntfyPollExecutorStub {
	return &ntfyPollExecutorStub{active: make(map[string]bool)}
}

func (s *ntfyPollExecutorStub) setActive(service string, up bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[service] = up
}

func (s *ntfyPollExecutorStub) IsServiceActive(_ context.Context, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[name], nil
}

func (s *ntfyPollExecutorStub) RestartService(context.Context, string) error { return nil }
func (s *ntfyPollExecutorStub) StartService(context.Context, string) error   { return nil }
func (s *ntfyPollExecutorStub) StopService(context.Context, string) error    { return nil }
func (s *ntfyPollExecutorStub) WriteConfig(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (s *ntfyPollExecutorStub) ReadConfig(context.Context, string) ([]byte, error) { return nil, nil }
func (s *ntfyPollExecutorStub) ApplySysctl(context.Context, string, string) error  { return nil }
func (s *ntfyPollExecutorStub) GetSysctl(context.Context, string) (string, error) {
	return "", nil
}
func (s *ntfyPollExecutorStub) SyncWireGuard(context.Context, string, []byte) error { return nil }
func (s *ntfyPollExecutorStub) RestartWireGuard(context.Context, string) error      { return nil }
func (s *ntfyPollExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return nil, nil
}
func (s *ntfyPollExecutorStub) EnableWireGuardInterface(context.Context, string) error  { return nil }
func (s *ntfyPollExecutorStub) DisableWireGuardInterface(context.Context, string) error { return nil }
func (s *ntfyPollExecutorStub) ValidateSingboxConfig(context.Context, []byte) error     { return nil }
func (s *ntfyPollExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}
func (s *ntfyPollExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *ntfyPollExecutorStub) Close() error                            { return nil }
func (s *ntfyPollExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

// recordingNtfyPublisher records every message it is asked to publish,
// synchronously, so tests never need time.Sleep to observe a notification.
type recordingNtfyPublisher struct {
	mu       sync.Mutex
	messages []core.NtfyMessage
}

func (r *recordingNtfyPublisher) publish(_ context.Context, _ core.NtfySettings, m core.NtfyMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, m)
	return nil
}

func (r *recordingNtfyPublisher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

func newNtfyTestServerWithStore(t *testing.T) (*Server, *core.Store) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &core.Config{
		EnableSingbox:    true,
		EnableWireGuard:  true,
		SamplerIntervalSec: 120,
	}
	exec := newNtfyPollExecutorStub()
	cfg.SetExecutor(exec)
	server := NewServer(store, cfg, exec)
	return server, store
}

func seedFullyEnabledNtfySettings(t *testing.T, store *core.Store, thresholdBytes int64) {
	t.Helper()
	settings := core.NtfySettings{
		ServerURL:             "https://ntfy.example.test",
		Topic:                 "ogs-swg-test",
		AuthMode:              "none",
		EnableSingboxDown:     true,
		EnableWireguardDown:   true,
		EnableHighTraffic:     true,
		EnableConfigErrors:    true,
		TrafficThresholdBytes: thresholdBytes,
	}
	if err := store.UpdateNtfySettings(context.Background(), settings); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}
}

func TestNtfyWiringStatusPollerObservesEnabledServicesOnly(t *testing.T) {
	server, store := newNtfyTestServerWithStore(t)
	seedFullyEnabledNtfySettings(t, store, 0)

	rec := &recordingNtfyPublisher{}
	server.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return store.GetNtfySettings(ctx) },
		rec.publish,
	)

	exec := server.executor.(*ntfyPollExecutorStub)
	exec.setActive("sing-box", true)
	exec.setActive("wireguard", true)

	ctx := context.Background()

	// First observation per service only seeds state; no message yet.
	server.pollNtfyServiceStatusOnce(ctx)
	if got := rec.count(); got != 0 {
		t.Fatalf("after seeding poll: messages=%d, want 0", got)
	}

	// Repeating the identical status must not re-fire (D-10).
	server.pollNtfyServiceStatusOnce(ctx)
	if got := rec.count(); got != 0 {
		t.Fatalf("after repeat identical poll: messages=%d, want 0", got)
	}

	// Flip sing-box down: exactly one down transition message.
	exec.setActive("sing-box", false)
	server.pollNtfyServiceStatusOnce(ctx)
	if got := rec.count(); got != 1 {
		t.Fatalf("after sing-box down transition: messages=%d, want 1", got)
	}

	// --- WireGuard disabled: no wireguard message ever produced ---
	server2, store2 := newNtfyTestServerWithStore(t)
	server2.config.EnableWireGuard = false
	seedFullyEnabledNtfySettings(t, store2, 0)
	rec2 := &recordingNtfyPublisher{}
	server2.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return store2.GetNtfySettings(ctx) },
		rec2.publish,
	)
	exec2 := server2.executor.(*ntfyPollExecutorStub)
	exec2.setActive("sing-box", true)
	exec2.setActive("wireguard", true)
	server2.pollNtfyServiceStatusOnce(ctx)
	exec2.setActive("wireguard", false)
	server2.pollNtfyServiceStatusOnce(ctx)
	for _, m := range rec2.messagesSnapshot() {
		if containsWireguard(m) {
			t.Fatalf("expected no wireguard message when EnableWireGuard=false, got %+v", m)
		}
	}
}

func (r *recordingNtfyPublisher) messagesSnapshot() []core.NtfyMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.NtfyMessage, len(r.messages))
	copy(out, r.messages)
	return out
}

func containsWireguard(m core.NtfyMessage) bool {
	return containsFold(m.Title, "wireguard") || containsFold(m.Message, "wireguard")
}

func containsFold(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if equalFold(s[i:i+len(substr)], substr) {
				return true
			}
		}
		return false
	})()
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func TestNtfyWiringTrafficCheckUsesCombinedTotal(t *testing.T) {
	server, store := newNtfyTestServerWithStore(t)
	seedFullyEnabledNtfySettings(t, store, 100)

	rec := &recordingNtfyPublisher{}
	server.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return store.GetNtfySettings(ctx) },
		rec.publish,
	)

	now := time.Now()
	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: now.Add(-10 * time.Second).Unix(), Uplink: 60, Downlink: 60},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	server.checkNtfyTrafficThreshold(now)
	if got := rec.count(); got != 1 {
		t.Fatalf("after first above-threshold check: messages=%d, want 1", got)
	}

	// D-16: calling again with the same still-above-threshold total must not re-fire.
	server.checkNtfyTrafficThreshold(now)
	if got := rec.count(); got != 1 {
		t.Fatalf("after repeat above-threshold check: messages=%d, want 1 (no re-fire)", got)
	}
}

func TestApplySingboxChangesNotifiesOnGenuineFailureOnly(t *testing.T) {
	t.Run("success arms crash window, publishes nothing", func(t *testing.T) {
		server, stub := newSingboxHandlerTestServer(`{"inbounds":[]}`)
		rec := &recordingNtfyPublisher{}
		server.ntfyNotifier = core.NewNtfyNotifier(
			func(ctx context.Context) (core.NtfySettings, error) {
				return core.NtfySettings{}, nil
			},
			rec.publish,
		)
		_ = stub

		req := httptest.NewRequest(http.MethodPost, "/api/singbox/apply", nil)
		rec2 := httptest.NewRecorder()
		server.handleApplySingboxChanges(rec2, req)

		if rec2.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec2.Code, rec2.Body.String())
		}
		if got := rec.count(); got != 0 {
			t.Fatalf("messages=%d, want 0", got)
		}
	})

	t.Run("restart-required with nil Err (no Clash API) publishes nothing", func(t *testing.T) {
		server, _ := newSingboxHandlerTestServer(`{}`)
		server.config.MarkSingboxPending()
		rec := &recordingNtfyPublisher{}
		server.ntfyNotifier = core.NewNtfyNotifier(
			func(ctx context.Context) (core.NtfySettings, error) {
				return core.NtfySettings{}, nil
			},
			rec.publish,
		)

		req := httptest.NewRequest(http.MethodPost, "/api/singbox/apply", nil)
		rec2 := httptest.NewRecorder()
		server.handleApplySingboxChanges(rec2, req)

		if rec2.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%q", rec2.Code, rec2.Body.String())
		}
		var body map[string]interface{}
		if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["restart_required"] != true {
			t.Fatalf("body=%+v, want restart_required=true", body)
		}
		if got := rec.count(); got != 0 {
			t.Fatalf("messages=%d, want 0 (D-15: no Clash API configured is benign)", got)
		}
	})

	t.Run("restart-required with cause (configured Clash API rejects reload) notifies once", func(t *testing.T) {
		fixtureJSON := `{
			"experimental": {
				"clash_api": {
					"external_controller": "127.0.0.1:1"
				}
			}
		}`
		server, _ := newSingboxHandlerTestServer(fixtureJSON)
		server.config.MarkSingboxPending()
		rec := &recordingNtfyPublisher{}
		server.ntfyNotifier = core.NewNtfyNotifier(
			func(ctx context.Context) (core.NtfySettings, error) {
				return core.NtfySettings{
					ServerURL:          "https://ntfy.example.test",
					Topic:              "ogs-swg-test",
					AuthMode:           "none",
					EnableConfigErrors: true,
				}, nil
			},
			rec.publish,
		)

		req := httptest.NewRequest(http.MethodPost, "/api/singbox/apply", nil)
		rec2 := httptest.NewRecorder()
		server.handleApplySingboxChanges(rec2, req)

		if rec2.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%q", rec2.Code, rec2.Body.String())
		}
		var body map[string]interface{}
		if err := json.NewDecoder(rec2.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["restart_required"] != true {
			t.Fatalf("body=%+v, want restart_required=true", body)
		}
		if got := rec.count(); got != 1 {
			t.Fatalf("messages=%d, want 1 (genuine Clash API failure must notify)", got)
		}
	})
}
