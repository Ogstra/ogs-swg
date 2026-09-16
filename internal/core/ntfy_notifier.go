package core

import (
	"context"
	"strings"
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

	// restartGraceUntil holds, per service, the time until which an observed
	// down or down->up transition is treated as an operator-initiated restart
	// and never notified (neither as down/recovered nor as crash-after-reload).
	// Without this, any panel-triggered restart (from "Apply changes" or the
	// explicit Restart button) that the status poller happens to catch mid-
	// bounce is indistinguishable from a genuine unexpected crash.
	restartGraceUntil map[string]time.Time

	// baseURL returns the panel's public base URL (e.g. "https://swg.example.com"),
	// used to build each message's Icon and Click link from the ClickPath a
	// builder sets. Defaults to a no-op returning "", so Icon/Click are simply
	// omitted until SetBaseURL is called (e.g. no subscription domain configured).
	baseURL func() string
}

// NewNtfyNotifier constructs a notifier with the given settings loader and
// publish function. Defaults: now = time.Now, crash window = 120s.
func NewNtfyNotifier(settings NtfySettingsFunc, publish NtfyPublishFunc) *NtfyNotifier {
	return &NtfyNotifier{
		settings:          settings,
		publish:           publish,
		now:               time.Now,
		crashWindow:       120 * time.Second,
		restartGraceUntil: make(map[string]time.Time),
		baseURL:           func() string { return "" },
	}
}

// SetBaseURL sets the function used to resolve the panel's public base URL
// for Icon/Click links (see baseURL field doc). Call once at wiring time;
// safe to call with a func that reads a live, operator-editable config value.
func (n *NtfyNotifier) SetBaseURL(f func() string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.baseURL = f
}

// IconURL resolves the panel's sing-box logo against the configured base URL,
// the same way send() does for every event-driven notification. Returns ""
// when no base URL is configured. Exposed for handlers (e.g. the operator
// "send test notification" action) that publish via core.PublishNtfy
// directly instead of through an Observe*/Notify* method.
func (n *NtfyNotifier) IconURL() string {
	n.mu.Lock()
	base := strings.TrimRight(n.baseURL(), "/")
	n.mu.Unlock()
	if base == "" {
		return ""
	}
	return base + "/sing-box-icon.png"
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
//
// If a base URL is configured (SetBaseURL), this also fills in Icon (always
// the panel's sing-box logo) and Click (baseURL + the message's ClickPath,
// if the builder set one) — centralized here so individual message builders
// stay pure functions with no config/URL knowledge of their own.
func (n *NtfyNotifier) send(ctx context.Context, enabled func(NtfySettings) bool, build func() NtfyMessage) {
	s, err := n.settings(ctx)
	if err != nil || !enabled(s) || !s.Configured() {
		return
	}
	msg := build()
	n.mu.Lock()
	base := strings.TrimRight(n.baseURL(), "/")
	n.mu.Unlock()
	if base != "" {
		msg.Icon = base + "/sing-box-icon.png"
		if msg.ClickPath != "" {
			msg.Click = base + msg.ClickPath
		}
	}
	_ = n.publish(ctx, s, msg)
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

	inGrace := n.now().Before(n.restartGraceUntil[service])

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

	if inGrace {
		// Operator-initiated restart in progress (Apply changes / Restart
		// button) — this down/up blip is expected, not a service incident.
		n.mu.Unlock()
		return
	}

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

// NotifyServiceRestarting marks the given service as undergoing an
// operator-initiated action (Restart, Stop, or the restart triggered by
// "Apply changes"), so the status poller's next down/up observations within
// the crash window are treated as expected rather than a
// down/recovered/crash-after-reload event. It also clears any pending
// config-apply crash correlation for sing-box, since this action supersedes
// it.
func (n *NtfyNotifier) NotifyServiceRestarting(service string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.restartGraceUntil[service] = n.now().Add(n.crashWindow)
	if service == NtfyServiceSingbox {
		n.lastApplyOK = time.Time{}
	}
}

// ObserveSubscriptionHWID publishes a new-device notification for a
// subscription request whose HWID was not previously known for that
// subscription. Unlike ObserveServiceStatus/ObserveTrafficTotal it holds no
// in-memory transition state and takes no lock: de-duplication already
// happened atomically in the store (Store.RecordSubscriptionHWIDFirstSeen
// only reports "new" once per (sub_id, hwid_hash)), so re-deriving it here
// would add a second, weaker source of truth. It never returns an error and
// never blocks: send() delegates to the injected publisher, which in the
// server is the async publisher from phase 56.
func (n *NtfyNotifier) ObserveSubscriptionHWID(ctx context.Context, info NtfyNewHWIDInfo) {
	n.send(ctx, func(s NtfySettings) bool { return s.EnableNewHwid }, func() NtfyMessage {
		return NtfyNewHWIDMessage(info)
	})
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
