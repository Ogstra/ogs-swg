package api

import (
	"context"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// slowDashboardExecutorStub delays the two calls that the dashboard status path
// issues, so a sequential implementation is distinguishable from a concurrent one.
type slowDashboardExecutorStub struct {
	dashboardExecutorStub // embed the existing stub for the no-op methods
	delay                 time.Duration
	serviceCalls          atomic.Int64
}

func (s *slowDashboardExecutorStub) IsServiceActive(ctx context.Context, name string) (bool, error) {
	s.serviceCalls.Add(1)
	time.Sleep(s.delay)
	return true, nil
}

func (s *slowDashboardExecutorStub) GetWireGuardStats(ctx context.Context) (map[string]core.PeerStats, error) {
	time.Sleep(s.delay)
	return nil, nil
}

func newConcurrencyTestServer(t *testing.T, enableSingbox, enableWireGuard bool, demoMode bool, exec core.SystemExecutor) *Server {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &core.Config{
		EnableSingbox:   enableSingbox,
		EnableWireGuard: enableWireGuard,
		DemoMode:        demoMode,
	}

	return NewServer(store, cfg, exec)
}

func TestCollectSystemStatus_ChecksRunConcurrently(t *testing.T) {
	delay := 150 * time.Millisecond
	exec := &slowDashboardExecutorStub{delay: delay}
	server := newConcurrencyTestServer(t, true, true, false, exec)

	start := time.Now()
	status := server.collectSystemStatus(context.Background(), 5*time.Minute)
	elapsed := time.Since(start)

	t.Logf("collectSystemStatus elapsed=%s (delay=%s per check)", elapsed, delay)

	if elapsed >= 350*time.Millisecond {
		t.Fatalf("collectSystemStatus took %s; expected concurrent execution to finish well under 350ms", elapsed)
	}

	if singbox, _ := status["singbox"].(bool); !singbox {
		t.Fatalf("singbox status = %v; want true", status["singbox"])
	}
	if wireguard, _ := status["wireguard"].(bool); !wireguard {
		t.Fatalf("wireguard status = %v; want true", status["wireguard"])
	}
}

func TestCollectSystemStatus_ReturnsSameKeysAndValues(t *testing.T) {
	exec := &dashboardExecutorStub{}
	server := newConcurrencyTestServer(t, true, true, false, exec)

	status := server.collectSystemStatus(context.Background(), 5*time.Minute)

	wantKeys := []string{
		"active_users_singbox", "active_users_singbox_list",
		"active_users_wireguard", "active_users_wireguard_list",
		"enable_singbox", "enable_wireguard", "singbox", "wireguard",
	}
	gotKeys := make([]string, 0, len(status))
	for k := range status {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sortedWant := append([]string(nil), wantKeys...)
	sort.Strings(sortedWant)

	if len(gotKeys) != len(sortedWant) {
		t.Fatalf("status keys = %v; want %v", gotKeys, sortedWant)
	}
	for i := range gotKeys {
		if gotKeys[i] != sortedWant[i] {
			t.Fatalf("status keys = %v; want %v", gotKeys, sortedWant)
		}
	}

	if _, ok := status["singbox"].(bool); !ok {
		t.Fatalf("singbox is not a bool: %T", status["singbox"])
	}
	if _, ok := status["wireguard"].(bool); !ok {
		t.Fatalf("wireguard is not a bool: %T", status["wireguard"])
	}
	if _, ok := status["active_users_singbox"].(int64); !ok {
		t.Fatalf("active_users_singbox is not int64: %T", status["active_users_singbox"])
	}
	if _, ok := status["active_users_wireguard"].(int); !ok {
		t.Fatalf("active_users_wireguard is not int: %T", status["active_users_wireguard"])
	}
	if _, ok := status["active_users_singbox_list"].([]string); !ok {
		t.Fatalf("active_users_singbox_list is not []string: %T", status["active_users_singbox_list"])
	}
	if _, ok := status["active_users_wireguard_list"].([]string); !ok {
		t.Fatalf("active_users_wireguard_list is not []string: %T", status["active_users_wireguard_list"])
	}
	if _, ok := status["enable_singbox"].(bool); !ok {
		t.Fatalf("enable_singbox is not a bool: %T", status["enable_singbox"])
	}
	if _, ok := status["enable_wireguard"].(bool); !ok {
		t.Fatalf("enable_wireguard is not a bool: %T", status["enable_wireguard"])
	}
}

func TestCollectSystemStatus_SingleServiceEnabled(t *testing.T) {
	t.Run("singbox only", func(t *testing.T) {
		exec := &dashboardExecutorStub{}
		server := newConcurrencyTestServer(t, true, false, false, exec)
		status := server.collectSystemStatus(context.Background(), 5*time.Minute)

		if wireguard, _ := status["wireguard"].(bool); wireguard {
			t.Fatalf("wireguard status = %v; want false", status["wireguard"])
		}
		if activeWG, _ := status["active_users_wireguard"].(int); activeWG != 0 {
			t.Fatalf("active_users_wireguard = %v; want 0", status["active_users_wireguard"])
		}
		if list, _ := status["active_users_wireguard_list"].([]string); list != nil {
			t.Fatalf("active_users_wireguard_list = %v; want nil", list)
		}
	})

	t.Run("wireguard only", func(t *testing.T) {
		exec := &dashboardExecutorStub{}
		server := newConcurrencyTestServer(t, false, true, false, exec)
		status := server.collectSystemStatus(context.Background(), 5*time.Minute)

		if singbox, _ := status["singbox"].(bool); singbox {
			t.Fatalf("singbox status = %v; want false", status["singbox"])
		}
		if activeSB, _ := status["active_users_singbox"].(int64); activeSB != 0 {
			t.Fatalf("active_users_singbox = %v; want 0", status["active_users_singbox"])
		}
		if list, _ := status["active_users_singbox_list"].([]string); list != nil {
			t.Fatalf("active_users_singbox_list = %v; want nil", list)
		}
	})
}

func TestCollectSystemStatus_DemoModeForcesBothTrue(t *testing.T) {
	exec := &dashboardExecutorStub{}
	server := newConcurrencyTestServer(t, true, true, true, exec)

	status := server.collectSystemStatus(context.Background(), 5*time.Minute)

	if singbox, _ := status["singbox"].(bool); !singbox {
		t.Fatalf("singbox status = %v; want true (demo mode)", status["singbox"])
	}
	if wireguard, _ := status["wireguard"].(bool); !wireguard {
		t.Fatalf("wireguard status = %v; want true (demo mode)", status["wireguard"])
	}
}
