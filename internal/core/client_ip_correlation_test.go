package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientIPCorrelation_ObserveNginxStreamLine(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	c.now = func() time.Time { return now }

	line := `client=198.51.100.24 remote_port=43123 upstream=127.0.0.1:443`
	if !c.ObserveNginxStreamLine(line) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	c.mu.RLock()
	entry, ok := c.entries["43123"]
	c.mu.RUnlock()
	if !ok {
		t.Fatal("entry for port 43123 not stored")
	}
	if entry.ip != "198.51.100.24" {
		t.Fatalf("entry.ip=%q, want %q", entry.ip, "198.51.100.24")
	}
}

func TestClientIPCorrelation_ObserveNginxStreamLine_SpaceSeparatedRemoteAddrAndPort(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	c.now = func() time.Time { return now }

	line := `198.51.100.24 43123 TCP 200 200 0 0 0.001 0.001 127.0.0.1:443`
	if !c.ObserveNginxStreamLine(line) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	if got := c.ResolveLoopbackRemote("127.0.0.1:43123"); got != "198.51.100.24" {
		t.Fatalf("ResolveLoopbackRemote=%q, want %q", got, "198.51.100.24")
	}
}

func TestClientIPCorrelation_ResolveLoopbackHit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	c.now = func() time.Time { return now }

	if !c.ObserveNginxStreamLine(`198.51.100.24:43123 accepted`) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	got := c.ResolveLoopbackRemote("127.0.0.1:43123")
	if got != "198.51.100.24" {
		t.Fatalf("ResolveLoopbackRemote=%q, want %q", got, "198.51.100.24")
	}
}

func TestClientIPCorrelation_ResolveLoopbackMissAfterExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClientIPCorrelation(5, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	c.now = func() time.Time { return now }

	if !c.ObserveNginxStreamLine(`198.51.100.24:43123 accepted`) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	c.now = func() time.Time { return now.Add(6 * time.Second) }
	if got := c.ResolveLoopbackRemote("127.0.0.1:43123"); got != "127.0.0.1:43123" {
		t.Fatalf("ResolveLoopbackRemote=%q, want original remote", got)
	}
}

func TestClientIPCorrelation_ResolveLoopbackPassesThroughDirectRemote(t *testing.T) {
	c := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	if got := c.ResolveLoopbackRemote("198.51.100.24:43123"); got != "198.51.100.24:43123" {
		t.Fatalf("ResolveLoopbackRemote=%q, want original remote", got)
	}
}

func TestClientIPCorrelation_IgnoreMalformedLine(t *testing.T) {
	c := NewClientIPCorrelation(defaultRealIPCacheTTLSec, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	if c.ObserveNginxStreamLine(`remote_port=43123 upstream=127.0.0.1:443`) {
		t.Fatal("ObserveNginxStreamLine=true, want false")
	}
	if got := c.ResolveLoopbackRemote("127.0.0.1:43123"); got != "127.0.0.1:43123" {
		t.Fatalf("ResolveLoopbackRemote=%q, want original remote", got)
	}
}

func TestClientIPCorrelation_CleanupExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClientIPCorrelation(5, defaultRealIPCleanupIntervalSec, defaultRealIPResolverMode, "")
	c.now = func() time.Time { return now }

	if !c.ObserveNginxStreamLine(`198.51.100.24:43123 accepted`) {
		t.Fatal("ObserveNginxStreamLine=false, want true")
	}

	c.now = func() time.Time { return now.Add(6 * time.Second) }
	c.CleanupExpired()

	c.mu.RLock()
	_, ok := c.entries["43123"]
	c.mu.RUnlock()
	if ok {
		t.Fatal("expired entry still present after cleanup")
	}
}

func TestClientIPCorrelation_RuntimeStartStop(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "stream.log")

	c := NewClientIPCorrelation(1, 1, defaultRealIPResolverMode, logPath)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }
	c.Start()
	defer c.Stop()

	if err := os.WriteFile(logPath, []byte("client=198.51.100.55 remote_port=45678 upstream=127.0.0.1:443\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	resolved := false
	for time.Now().Before(deadline) {
		if got := c.ResolveLoopbackRemote("127.0.0.1:45678"); got == "198.51.100.55" {
			c.now = func() time.Time { return now.Add(2 * time.Second) }
			resolved = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !resolved {
		t.Fatal("runtime poller did not ingest appended nginx stream line")
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.RLock()
		_, ok := c.entries["45678"]
		c.mu.RUnlock()
		if !ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("runtime cleanup did not remove expired entry")
}
