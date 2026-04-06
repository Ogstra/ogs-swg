package core

import (
	"testing"
	"time"
)

func TestWatcherRealIP_UsesResolvedLoopbackSource(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	resolver := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	resolver.now = func() time.Time { return now }
	if !resolver.ObserveNginxStreamLine(`client=198.51.100.44 remote_port=45678 upstream=127.0.0.1:443`) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	watcher := NewWatcher("", resolver)
	watcher.now = func() time.Time { return now }
	watcher.processLine(`time=2026-04-05T00:00:00Z inbound connection from 127.0.0.1:45678 email:alice@example.com`)

	active := watcher.GetActiveConnections(60)
	if len(active) != 1 {
		t.Fatalf("len(active)=%d want 1", len(active))
	}
	if active[0].User != "alice@example.com" {
		t.Fatalf("user=%q want %q", active[0].User, "alice@example.com")
	}
	if active[0].SourceIP != "198.51.100.44" {
		t.Fatalf("source_ip=%q want %q", active[0].SourceIP, "198.51.100.44")
	}
	if active[0].SourcePort != "45678" {
		t.Fatalf("source_port=%q want %q", active[0].SourcePort, "45678")
	}

	users := watcher.GetActiveUsers(60)
	if len(users) != 1 || users[0] != "alice@example.com" {
		t.Fatalf("GetActiveUsers=%v want [%q]", users, "alice@example.com")
	}
}

func TestWatcherRealIP_PreservesDirectSource(t *testing.T) {
	watcher := NewWatcher("")
	watcher.now = func() time.Time { return time.Unix(1_700_000_100, 0) }
	watcher.processLine(`time=2026-04-05T00:00:00Z inbound connection from 198.51.100.45:45678 email:bob@example.com`)

	active := watcher.GetActiveConnections(60)
	if len(active) != 1 {
		t.Fatalf("len(active)=%d want 1", len(active))
	}
	if active[0].SourceIP != "198.51.100.45" {
		t.Fatalf("source_ip=%q want %q", active[0].SourceIP, "198.51.100.45")
	}
}

func TestWatcherRealIP_PreservesLoopbackOnCorrelationMiss(t *testing.T) {
	watcher := NewWatcher("", NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, ""))
	watcher.now = func() time.Time { return time.Unix(1_700_000_100, 0) }
	watcher.processLine(`time=2026-04-05T00:00:00Z inbound connection from 127.0.0.1:45678 email:carol@example.com`)

	active := watcher.GetActiveConnections(60)
	if len(active) != 1 {
		t.Fatalf("len(active)=%d want 1", len(active))
	}
	if active[0].SourceIP != "127.0.0.1" {
		t.Fatalf("source_ip=%q want %q", active[0].SourceIP, "127.0.0.1")
	}
}
