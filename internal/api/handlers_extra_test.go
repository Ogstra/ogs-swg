package api

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestHandleRestartService_SingboxDispatchesAsync(t *testing.T) {
	prevDelay := detachedServiceActionDelay
	detachedServiceActionDelay = 0
	defer func() { detachedServiceActionDelay = prevDelay }()

	stub := newServiceActionExecutorStub()
	cfg := &core.Config{EnableSingbox: true}
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

func TestHandleGetFeatures_RealIP(t *testing.T) {
	cfg := &core.Config{
		EnableSingbox:            true,
		EnableWireGuard:          true,
		RealIPCorrelationEnabled: true,
		RealIPNginxStreamLogPath: "/var/log/nginx/stream.log",
		RealIPCacheTTLSec:        30,
		RealIPCleanupIntervalSec: 60,
		RealIPResolverMode:       "loopback_only",
	}
	server := NewServer(nil, cfg, newServiceActionExecutorStub())

	req := httptest.NewRequest(http.MethodGet, "/api/settings/features", nil)
	rec := httptest.NewRecorder()
	server.handleGetFeatures(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got := payload["real_ip_correlation_enabled"]; got != true {
		t.Fatalf("real_ip_correlation_enabled=%v want true", got)
	}
	if got := payload["real_ip_nginx_stream_log_path"]; got != "/var/log/nginx/stream.log" {
		t.Fatalf("real_ip_nginx_stream_log_path=%v want %q", got, "/var/log/nginx/stream.log")
	}
	if got := payload["real_ip_cache_ttl_sec"]; got != float64(30) {
		t.Fatalf("real_ip_cache_ttl_sec=%v want 30", got)
	}
	if got := payload["real_ip_cleanup_interval_sec"]; got != float64(60) {
		t.Fatalf("real_ip_cleanup_interval_sec=%v want 60", got)
	}
	if got := payload["real_ip_resolver_mode"]; got != "loopback_only" {
		t.Fatalf("real_ip_resolver_mode=%v want %q", got, "loopback_only")
	}
}

func TestHandleUpdateFeatures_RealIP(t *testing.T) {
	cfg := &core.Config{
		ConfigPath:               "",
		EnableSingbox:            true,
		RealIPNginxStreamLogPath: "/var/log/nginx/stream.log",
		RealIPCacheTTLSec:        30,
		RealIPCleanupIntervalSec: 60,
		RealIPResolverMode:       "loopback_only",
	}
	server := NewServer(nil, cfg, newServiceActionExecutorStub())

	body := bytes.NewBufferString(`{
		"real_ip_correlation_enabled": true,
		"real_ip_nginx_stream_log_path": " /tmp/nginx-stream.log ",
		"real_ip_cache_ttl_sec": 15,
		"real_ip_cleanup_interval_sec": 120,
		"real_ip_resolver_mode": "direct_override"
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/features", body)
	rec := httptest.NewRecorder()
	server.handleUpdateFeatures(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if !server.config.RealIPCorrelationEnabled {
		t.Fatal("RealIPCorrelationEnabled=false want true")
	}
	if got := server.config.RealIPNginxStreamLogPath; got != "/tmp/nginx-stream.log" {
		t.Fatalf("RealIPNginxStreamLogPath=%q want %q", got, "/tmp/nginx-stream.log")
	}
	if got := server.config.RealIPCacheTTLSec; got != 15 {
		t.Fatalf("RealIPCacheTTLSec=%d want 15", got)
	}
	if got := server.config.RealIPCleanupIntervalSec; got != 120 {
		t.Fatalf("RealIPCleanupIntervalSec=%d want 120", got)
	}
	if got := server.config.RealIPResolverMode; got != "loopback_only" {
		t.Fatalf("RealIPResolverMode=%q want %q", got, "loopback_only")
	}
	if server.realIPResolver == nil {
		t.Fatal("realIPResolver=nil want initialized after enabling feature")
	}

	server.realIPResolver.ObserveNginxStreamLine(`client=198.51.100.44 remote_port=45678 upstream=127.0.0.1:443`)
	reqIP := httptest.NewRequest(http.MethodGet, "/s/test", nil)
	reqIP.RemoteAddr = "127.0.0.1:45678"
	if got := server.resolveSubscriptionRequestIP(reqIP); got != "198.51.100.44" {
		t.Fatalf("resolveSubscriptionRequestIP=%q want %q", got, "198.51.100.44")
	}
}
