package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCalculatorRealIP_UsesActiveConnectionsPath(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(filepath.Join(tmp, "samples.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	watcher := NewWatcher("")
	now := time.Unix(1_700_000_100, 0)
	watcher.now = func() time.Time { return now }
	watcher.processLine(`time=2026-04-05T00:00:00Z inbound connection from 198.51.100.44:45678 email:alice@example.com`)

	calc := NewCalculator(watcher, nil, store, []string{"test-inbound"})
	calc.now = func() time.Time { return now }
	trafficCalls := 0
	calc.getTraffic = func(tags []string) (int64, int64, error) {
		trafficCalls++
		if trafficCalls == 1 {
			return 100, 200, nil
		}
		return 160, 260, nil
	}

	calc.process()
	calc.process()

	samples, err := store.GetCombinedReport("alice@example.com", now.Add(-time.Minute).Unix(), now.Add(time.Minute).Unix())
	if err != nil {
		t.Fatalf("GetCombinedReport: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples)=%d want 1", len(samples))
	}
	if samples[0].Uplink != 60 || samples[0].Downlink != 60 {
		t.Fatalf("sample=%+v want uplink=60 downlink=60", samples[0])
	}

	connections := watcher.GetActiveConnections(60)
	if len(connections) != 1 || connections[0].SourceIP != "198.51.100.44" {
		t.Fatalf("connections=%+v want resolved metadata", connections)
	}
}
