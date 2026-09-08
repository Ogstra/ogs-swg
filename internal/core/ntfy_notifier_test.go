package core

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type recordedPublish struct {
	Settings NtfySettings
	Message  NtfyMessage
}

// newTestNotifier builds a notifier wired to an injected fake settings
// source (mutable via the returned pointer so a subtest can flip a toggle
// mid-test), a fake publisher that records every call, and a controllable
// clock. No real HTTP, timers, or sleeping are involved.
func newTestNotifier(t *testing.T, s NtfySettings) (*NtfyNotifier, *[]recordedPublish, *time.Time) {
	t.Helper()

	var mu sync.Mutex
	current := s
	recorded := []recordedPublish{}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	settingsFn := func(ctx context.Context) (NtfySettings, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	publishFn := func(ctx context.Context, s NtfySettings, m NtfyMessage) error {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, recordedPublish{Settings: s, Message: m})
		return nil
	}

	n := NewNtfyNotifier(settingsFn, publishFn)
	n.SetNow(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	})

	// Expose setter for the toggle via closure capture in tests that need it.
	t.Cleanup(func() {})

	return n, &recorded, &clock
}

func baseSettings() NtfySettings {
	return NtfySettings{
		ServerURL:             "https://ntfy.example.test",
		Topic:                 "panel-events",
		AuthMode:              "none",
		EnableSingboxDown:     true,
		EnableWireguardDown:   true,
		EnableHighTraffic:     true,
		EnableConfigErrors:    true,
		TrafficThresholdBytes: 1000,
	}
}

// mutableNotifier wraps the harness with a settings pointer the test can
// mutate directly (bypassing the mutex is fine here: single-goroutine tests).
func newMutableNotifier(t *testing.T, s NtfySettings) (*NtfyNotifier, *[]recordedPublish, *NtfySettings) {
	t.Helper()

	var mu sync.Mutex
	current := s
	recorded := []recordedPublish{}

	settingsFn := func(ctx context.Context) (NtfySettings, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	publishFn := func(ctx context.Context, s NtfySettings, m NtfyMessage) error {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, recordedPublish{Settings: s, Message: m})
		return nil
	}

	n := NewNtfyNotifier(settingsFn, publishFn)
	return n, &recorded, &current
}

// ---------------------------------------------------------------------------
// Service status transitions (D-10)
// ---------------------------------------------------------------------------

func TestNtfyNotifierSeedsWithoutNotifying(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)
	if len(*recorded) != 0 {
		t.Fatalf("first observation published %d messages; want 0 (seed only)", len(*recorded))
	}
}

func TestNtfyNotifierDownThenUpFiresOncePerTransition(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)
	if len(*recorded) != 1 {
		t.Fatalf("after down transition: %d messages; want 1", len(*recorded))
	}
	down := (*recorded)[0].Message
	want := NtfyServiceDownMessage(NtfyServiceSingbox)
	if down.Title != want.Title || !slices.Equal(down.Tags, want.Tags) || down.Priority != want.Priority {
		t.Fatalf("down message = %+v; want %+v", down, want)
	}

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)
	if len(*recorded) != 2 {
		t.Fatalf("after up transition: %d messages; want 2", len(*recorded))
	}
	up := (*recorded)[1].Message
	wantUp := NtfyServiceRecoveredMessage(NtfyServiceSingbox)
	if up.Title != wantUp.Title || !slices.Equal(up.Tags, wantUp.Tags) || up.Priority != wantUp.Priority {
		t.Fatalf("recovered message = %+v; want %+v", up, wantUp)
	}
}

func TestNtfyNotifierRepeatedDownObservationsDoNotResend(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)

	if len(*recorded) != 1 {
		t.Fatalf("repeated down observations published %d messages; want 1", len(*recorded))
	}
}

func TestNtfyNotifierServicesAreIndependent(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)
	n.ObserveServiceStatus(ctx, NtfyServiceWireGuard, true)

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)
	if len(*recorded) != 1 {
		t.Fatalf("sing-box down published %d messages; want 1", len(*recorded))
	}
	if (*recorded)[0].Message.Title != NtfyServiceDownMessage(NtfyServiceSingbox).Title {
		t.Fatalf("expected sing-box down message, got %+v", (*recorded)[0].Message)
	}

	// WireGuard must be unaffected by the sing-box transition.
	n.ObserveServiceStatus(ctx, NtfyServiceWireGuard, false)
	if len(*recorded) != 2 {
		t.Fatalf("after wireguard down: %d messages; want 2", len(*recorded))
	}
	if (*recorded)[1].Message.Title != NtfyServiceDownMessage(NtfyServiceWireGuard).Title {
		t.Fatalf("expected wireguard down message, got %+v", (*recorded)[1].Message)
	}
}

func TestNtfyNotifierToggleSuppressesEventButKeepsState(t *testing.T) {
	n, recorded, settings := newMutableNotifier(t, baseSettings())
	ctx := context.Background()

	settings.EnableSingboxDown = false

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false) // suppressed, but state advances
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)  // suppressed, state advances

	if len(*recorded) != 0 {
		t.Fatalf("with toggle off: %d messages; want 0", len(*recorded))
	}

	settings.EnableSingboxDown = true
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)

	if len(*recorded) != 1 {
		t.Fatalf("after re-enable + down: %d messages; want 1", len(*recorded))
	}
	if (*recorded)[0].Message.Title != NtfyServiceDownMessage(NtfyServiceSingbox).Title {
		t.Fatalf("expected the post-re-enable down message, got %+v", (*recorded)[0].Message)
	}
}

func TestNtfyNotifierUnconfiguredPublishesNothing(t *testing.T) {
	t.Run("empty topic", func(t *testing.T) {
		s := baseSettings()
		s.Topic = ""
		n, recorded, _ := newTestNotifier(t, s)
		ctx := context.Background()

		n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)
		n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)

		if len(*recorded) != 0 {
			t.Fatalf("with empty topic: %d messages; want 0", len(*recorded))
		}
	})

	t.Run("empty server url", func(t *testing.T) {
		s := baseSettings()
		s.ServerURL = ""
		n, recorded, _ := newTestNotifier(t, s)
		ctx := context.Background()

		n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true)
		n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false)

		if len(*recorded) != 0 {
			t.Fatalf("with empty server URL: %d messages; want 0", len(*recorded))
		}
	})
}

// ---------------------------------------------------------------------------
// Traffic threshold transitions (D-16, D-11)
// ---------------------------------------------------------------------------

func TestNtfyNotifierTrafficThresholdTransitionOnly(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()
	window := 5 * time.Minute

	n.ObserveTrafficTotal(ctx, 500, window) // below
	if len(*recorded) != 0 {
		t.Fatalf("below threshold: %d messages; want 0", len(*recorded))
	}

	n.ObserveTrafficTotal(ctx, 1500, window) // below -> above: fires
	if len(*recorded) != 1 {
		t.Fatalf("first above-threshold crossing: %d messages; want 1", len(*recorded))
	}

	n.ObserveTrafficTotal(ctx, 2000, window) // sustained above: no re-fire
	if len(*recorded) != 1 {
		t.Fatalf("sustained above threshold: %d messages; want still 1", len(*recorded))
	}

	n.ObserveTrafficTotal(ctx, 200, window) // drop below: re-arms, no fire
	if len(*recorded) != 1 {
		t.Fatalf("drop below threshold: %d messages; want still 1", len(*recorded))
	}

	n.ObserveTrafficTotal(ctx, 1600, window) // crosses again: fires
	if len(*recorded) != 2 {
		t.Fatalf("second above-threshold crossing: %d messages; want 2", len(*recorded))
	}
}

func TestNtfyNotifierTrafficThresholdZeroDisabled(t *testing.T) {
	s := baseSettings()
	s.TrafficThresholdBytes = 0
	n, recorded, _ := newTestNotifier(t, s)
	ctx := context.Background()

	n.ObserveTrafficTotal(ctx, 999999, 5*time.Minute)
	n.ObserveTrafficTotal(ctx, 999999, 5*time.Minute)

	if len(*recorded) != 0 {
		t.Fatalf("with threshold=0: %d messages; want 0 (disabled)", len(*recorded))
	}
}

// ---------------------------------------------------------------------------
// Crash-after-reload correlation (D-05, D-15)
// ---------------------------------------------------------------------------

func TestNtfyNotifierCrashAfterReloadWindow(t *testing.T) {
	cases := []struct {
		name        string
		sinceReload time.Duration
		wantCrash   bool
	}{
		{"1s after reload", 1 * time.Second, true},
		{"119s after reload", 119 * time.Second, true},
		{"121s after reload", 121 * time.Second, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, recorded, _ := newTestNotifier(t, baseSettings())
			ctx := context.Background()

			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			cur := base
			n.SetNow(func() time.Time { return cur })

			n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed up
			n.NotifyConfigApplySucceeded()                        // reload at t=base

			cur = base.Add(tc.sinceReload)
			n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false) // down transition

			if len(*recorded) != 1 {
				t.Fatalf("%s: got %d messages; want 1", tc.name, len(*recorded))
			}
			msg := (*recorded)[0].Message
			if tc.wantCrash {
				if !slices.Equal(msg.Tags, []string{"skull", "gear"}) {
					t.Fatalf("%s: tags = %q; want crash tags %q", tc.name, msg.Tags, []string{"skull", "gear"})
				}
			} else {
				if !slices.Equal(msg.Tags, []string{"rotating_light", "warning"}) {
					t.Fatalf("%s: tags = %q; want generic-down tags %q", tc.name, msg.Tags, []string{"rotating_light", "warning"})
				}
			}
		})
	}
}

func TestNtfyNotifierCrashRequiresConfigErrorsToggle(t *testing.T) {
	s := baseSettings()
	s.EnableConfigErrors = false
	s.EnableSingboxDown = true
	n, recorded, _ := newTestNotifier(t, s)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cur := base
	n.SetNow(func() time.Time { return cur })

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed up
	n.NotifyConfigApplySucceeded()

	cur = base.Add(1 * time.Second)
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false) // within window, but config-errors off

	if len(*recorded) != 0 {
		t.Fatalf("crash path with EnableConfigErrors=false: %d messages; want 0 (must not fall through to generic down)", len(*recorded))
	}
}

func TestNtfyNotifierConfigApplyFailedAlwaysSends(t *testing.T) {
	n, recorded, _ := newTestNotifier(t, baseSettings())
	ctx := context.Background()

	n.NotifyConfigApplyFailed(ctx, "detail one")
	n.NotifyConfigApplyFailed(ctx, "detail two")
	n.NotifyConfigApplyFailed(ctx, "detail three")

	if len(*recorded) != 3 {
		t.Fatalf("three calls to NotifyConfigApplyFailed published %d messages; want 3", len(*recorded))
	}
}

// ---------------------------------------------------------------------------
// Publish error resilience
// ---------------------------------------------------------------------------

func TestNtfyNotifierPublishErrorDoesNotBlockState(t *testing.T) {
	calls := 0
	settingsFn := func(ctx context.Context) (NtfySettings, error) {
		return baseSettings(), nil
	}
	publishFn := func(ctx context.Context, s NtfySettings, m NtfyMessage) error {
		calls++
		return errors.New("simulated publish failure")
	}
	n := NewNtfyNotifier(settingsFn, publishFn)
	ctx := context.Background()

	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, true) // seed
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false) // publish attempt (fails)
	n.ObserveServiceStatus(ctx, NtfyServiceSingbox, false) // no transition; must not re-attempt publish

	if calls != 1 {
		t.Fatalf("publish attempted %d times; want 1 (state must advance despite publish error)", calls)
	}
}
