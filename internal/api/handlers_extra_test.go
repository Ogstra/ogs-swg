package api

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type serviceActionExecutorStub struct {
	mu sync.Mutex

	restartCalls []string
	stopCalls    []string

	restartStarted chan struct{}
	stopStarted    chan struct{}
	restartRelease chan struct{}
	stopRelease    chan struct{}

	restartStartOnce sync.Once
	stopStartOnce    sync.Once
}

func newServiceActionExecutorStub() *serviceActionExecutorStub {
	return &serviceActionExecutorStub{
		restartStarted: make(chan struct{}),
		stopStarted:    make(chan struct{}),
		restartRelease: make(chan struct{}),
		stopRelease:    make(chan struct{}),
	}
}

func (s *serviceActionExecutorStub) RestartService(ctx context.Context, name string) error {
	s.mu.Lock()
	s.restartCalls = append(s.restartCalls, name)
	release := s.restartRelease
	s.mu.Unlock()

	s.restartStartOnce.Do(func() { close(s.restartStarted) })

	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *serviceActionExecutorStub) StartService(context.Context, string) error { return nil }

func (s *serviceActionExecutorStub) StopService(ctx context.Context, name string) error {
	s.mu.Lock()
	s.stopCalls = append(s.stopCalls, name)
	release := s.stopRelease
	s.mu.Unlock()

	s.stopStartOnce.Do(func() { close(s.stopStarted) })

	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *serviceActionExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return false, nil
}

func (s *serviceActionExecutorStub) WriteConfig(context.Context, string, []byte, os.FileMode) error {
	return nil
}

func (s *serviceActionExecutorStub) ReadConfig(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }

func (s *serviceActionExecutorStub) GetSysctl(context.Context, string) (string, error) {
	return "", nil
}

func (s *serviceActionExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) ReadAllJournal(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) SyncWireGuard(context.Context, string, []byte) error { return nil }

func (s *serviceActionExecutorStub) RestartWireGuard(context.Context, string) error { return nil }

func (s *serviceActionExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) EnableWireGuardInterface(context.Context, string) error {
	return nil
}

func (s *serviceActionExecutorStub) DisableWireGuardInterface(context.Context, string) error {
	return nil
}

func (s *serviceActionExecutorStub) ValidateSingboxConfig(context.Context, []byte) error { return nil }

func (s *serviceActionExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}

func (s *serviceActionExecutorStub) CheckConnectivity(context.Context) error { return nil }

func (s *serviceActionExecutorStub) Close() error { return nil }

func (s *serviceActionExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func waitForClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration, msg string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(msg)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, msg string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestHandleRestartService_SingboxDispatchesAsync(t *testing.T) {
	prevDelay := detachedServiceActionDelay
	detachedServiceActionDelay = 0
	defer func() { detachedServiceActionDelay = prevDelay }()

	stub := newServiceActionExecutorStub()
	cfg := &core.Config{EnableSingbox: true}
	cfg.MarkSingboxPending()
	server := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodPost, "/api/service/restart", bytes.NewBufferString(`{"service":"sing-box"}`))
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		server.handleRestartService(rec, req)
		close(done)
	}()

	waitForClosed(t, done, 100*time.Millisecond, "restart handler blocked on sing-box executor")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	waitForClosed(t, stub.restartStarted, 100*time.Millisecond, "restart executor was not dispatched")
	close(stub.restartRelease)
	waitForCondition(t, 100*time.Millisecond, "sing-box pending changes were not cleared after restart", func() bool {
		return !cfg.GetSingboxPendingChanges()
	})
}

func TestHandleStopService_SingboxDispatchesAsync(t *testing.T) {
	prevDelay := detachedServiceActionDelay
	detachedServiceActionDelay = 0
	defer func() { detachedServiceActionDelay = prevDelay }()

	stub := newServiceActionExecutorStub()
	cfg := &core.Config{EnableSingbox: true}
	server := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodPost, "/api/service/stop", bytes.NewBufferString(`{"service":"sing-box"}`))
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		server.handleStopService(rec, req)
		close(done)
	}()

	waitForClosed(t, done, 100*time.Millisecond, "stop handler blocked on sing-box executor")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	waitForClosed(t, stub.stopStarted, 100*time.Millisecond, "stop executor was not dispatched")
	close(stub.stopRelease)
}

func TestHandleRestartService_WireGuardRemainsSynchronousAndClearsPending(t *testing.T) {
	prevDelay := detachedServiceActionDelay
	detachedServiceActionDelay = 0
	defer func() { detachedServiceActionDelay = prevDelay }()

	stub := newServiceActionExecutorStub()
	cfg := &core.Config{EnableWireGuard: true}
	server := NewServer(nil, cfg, stub)
	server.wgPendingRestart = true

	req := httptest.NewRequest(http.MethodPost, "/api/service/restart", bytes.NewBufferString(`{"service":"wireguard"}`))
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		server.handleRestartService(rec, req)
		close(done)
	}()

	waitForClosed(t, stub.restartStarted, 100*time.Millisecond, "wireguard restart did not start")

	select {
	case <-done:
		t.Fatal("wireguard restart returned before executor completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(stub.restartRelease)
	waitForClosed(t, done, 100*time.Millisecond, "wireguard restart handler did not finish")

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if server.wgPendingRestart {
		t.Fatal("wgPendingRestart was not cleared after wireguard restart")
	}
}
