package core

import (
	"context"
	"sync"
	"time"
)

// Service identifiers understood by NtfyNotifier.ObserveServiceStatus.
const (
	NtfyServiceSingbox   = "sing-box"
	NtfyServiceWireGuard = "wireguard"
)

// NtfyPublishFunc publishes a single ntfy message using the given settings.
// Errors are expected to be logged by the implementation; the notifier
// swallows them and does not let a publish failure block state advancement.
type NtfyPublishFunc func(ctx context.Context, s NtfySettings, m NtfyMessage) error

// NtfySettingsFunc loads the current ntfy settings. It is called on every
// observed event (not cached) so operator edits to toggles/thresholds take
// effect immediately without a panel restart (D-07).
type NtfySettingsFunc func(ctx context.Context) (NtfySettings, error)

// NtfyNotifier is the pure decision layer for every ntfy notification event:
// transition-only service up/down, transition-only high-traffic threshold,
// and config-apply-failure / crash-after-reload correlation. It has no HTTP
// or timer dependency, making every anti-spam rule unit-testable with an
// injected publisher and clock.
type NtfyNotifier struct {
	mu sync.Mutex

	settings NtfySettingsFunc
	publish  NtfyPublishFunc
	now      func() time.Time

	crashWindow time.Duration // default 120 * time.Second

	singboxUp   *bool // nil = not yet observed (seed state)
	wireguardUp *bool

	trafficAbove bool

	lastApplyOK time.Time // zero = no successful config apply observed yet
}

// NewNtfyNotifier constructs a notifier with the given settings loader and
// publish function. Defaults: now = time.Now, crash window = 120s.
func NewNtfyNotifier(settings NtfySettingsFunc, publish NtfyPublishFunc) *NtfyNotifier {
	return &NtfyNotifier{
		settings:    settings,
		publish:     publish,
		now:         time.Now,
		crashWindow: 120 * time.Second,
	}
}

// SetNow overrides the clock used for crash-window correlation. Test seam.
func (n *NtfyNotifier) SetNow(now func() time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.now = now
}

// SetCrashWindow overrides the crash-after-reload correlation window. Test seam.
func (n *NtfyNotifier) SetCrashWindow(d time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.crashWindow = d
}

// send loads live settings, checks the per-event toggle and Configured(),
// then publishes. It is a no-op (no publish) whenever settings fail to load,
// the event's toggle is off, or the ntfy server URL/topic is not configured.
func (n *NtfyNotifier) send(ctx context.Context, enabled func(NtfySettings) bool, build func() NtfyMessage) {
	s, err := n.settings(ctx)
	if err != nil || !enabled(s) || !s.Configured() {
		return
	}
	_ = n.publish(ctx, s, build())
}

// ObserveServiceStatus records the latest observed up/down status for a
// monitored service and publishes a notification only on a genuine
// transition (D-10: repeated identical observations never re-fire). The
// first-ever observation for a service seeds state without notifying.
func (n *NtfyNotifier) ObserveServiceStatus(ctx context.Context, service string, up bool) {
	n.mu.Lock()

	var ptr **bool
	var toggle func(NtfySettings) bool
	switch service {
	case NtfyServiceSingbox:
		ptr = &n.singboxUp
		toggle = func(s NtfySettings) bool { return s.EnableSingboxDown }
	case NtfyServiceWireGuard:
		ptr = &n.wireguardUp
		toggle = func(s NtfySettings) bool { return s.EnableWireguardDown }
	default:
		n.mu.Unlock()
		return
	}

	if *ptr == nil {
		v := up
		*ptr = &v
		n.mu.Unlock()
		return
	}

	if **ptr == up {
		n.mu.Unlock()
		return
	}

	v := up
	*ptr = &v

	crash := service == NtfyServiceSingbox && !up &&
		!n.lastApplyOK.IsZero() && n.now().Sub(n.lastApplyOK) <= n.crashWindow
	var since time.Duration
	if crash {
		since = n.now().Sub(n.lastApplyOK)
	}

	// Release the lock before publishing so a slow injected publisher cannot
	// block subsequent observations.
	n.mu.Unlock()

	switch {
	case crash:
		n.send(ctx, func(s NtfySettings) bool { return s.EnableConfigErrors }, func() NtfyMessage {
			return NtfyCrashAfterReloadMessage(since)
		})
	case up:
		n.send(ctx, toggle, func() NtfyMessage { return NtfyServiceRecoveredMessage(service) })
	default:
		n.send(ctx, toggle, func() NtfyMessage { return NtfyServiceDownMessage(service) })
	}
}

// ObserveTrafficTotal checks the combined traffic total against the
// configured threshold and publishes only on the below->above transition
// (D-16). Any below-threshold observation re-arms the alert. A threshold of
// 0 disables the check entirely (D-11).
func (n *NtfyNotifier) ObserveTrafficTotal(ctx context.Context, totalBytes int64, window time.Duration) {
	s, err := n.settings(ctx)
	if err != nil {
		return
	}

	n.mu.Lock()
	if s.TrafficThresholdBytes <= 0 {
		n.trafficAbove = false
		n.mu.Unlock()
		return
	}

	above := totalBytes >= s.TrafficThresholdBytes
	fire := above && !n.trafficAbove
	n.trafficAbove = above
	n.mu.Unlock()

	if !fire {
		return
	}

	n.send(ctx, func(s NtfySettings) bool { return s.EnableHighTraffic }, func() NtfyMessage {
		return NtfyHighTrafficMessage(totalBytes, s.TrafficThresholdBytes, window)
	})
}

// NotifyConfigApplySucceeded records the time of a successful sing-box
// config apply so a subsequent sing-box down transition within the crash
// window can be correlated as a crash-after-reload rather than a generic
// service-down event. It never publishes anything itself.
func (n *NtfyNotifier) NotifyConfigApplySucceeded() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastApplyOK = n.now()
}

// NotifyConfigApplyFailed publishes a config-apply-failed notification every
// time it is called (no dedupe — this represents a distinct operator-facing
// action/error, not a poll result). Callers are responsible for not calling
// this for a SingboxRestartRequiredError whose Err is nil (D-15).
func (n *NtfyNotifier) NotifyConfigApplyFailed(ctx context.Context, detail string) {
	n.send(ctx, func(s NtfySettings) bool { return s.EnableConfigErrors }, func() NtfyMessage {
		return NtfyConfigApplyFailedMessage(detail)
	})
}
