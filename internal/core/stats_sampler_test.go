package core

import (
	"testing"
	"time"
)

// TestStatsSamplerOnSampled verifies the SetOnSampled seam (D-04): the hook
// fires exactly once per completed non-paused sampling pass, and does not
// fire at all while the sampler is paused.
func TestStatsSamplerOnSampled(t *testing.T) {
	cfg := &Config{SingboxConfigPath: "/nonexistent/config.json"}
	client := NewSingboxClient("127.0.0.1:0", nil)
	sampler := NewStatsSampler(client, nil, cfg)

	fired := make(chan time.Time, 1)
	sampler.SetOnSampled(func(at time.Time) {
		fired <- at
	})

	sampler.TriggerOnce()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("onSampled hook did not fire within timeout")
	}

	// Paused: the hook must not fire.
	sampler.SetPaused(true)
	sampler.TriggerOnce()

	select {
	case at := <-fired:
		t.Fatalf("onSampled fired while paused: %v", at)
	case <-time.After(200 * time.Millisecond):
		// expected: no fire
	}
}
