package api

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// ntfyStatusPollInterval is the dedicated background poll cadence used to
// detect sing-box/WireGuard service up/down transitions (NTFY-01). It is
// intentionally decoupled from any browser-facing status handler so that
// dashboard polling can never cause duplicate or missed notifications.
const ntfyStatusPollInterval = 60 * time.Second

// ntfyPollTimeout bounds one poll, including the re-checks of a service that
// looked down.
const ntfyPollTimeout = 45 * time.Second

// A service that looks down is checked again this many times, ntfyDownRecheckDelay
// apart, before it is reported. One failed or unlucky check (a service
// mid-restart, a busy systemd) would otherwise raise a "down" alert and a
// "recovered" one a poll later while nothing was wrong.
const ntfyDownRechecks = 2

var ntfyDownRecheckDelay = 3 * time.Second

// newNtfyAsyncPublisher returns a publisher that hands the HTTP POST to a
// goroutine so no request handler, poller, or sampler pass ever waits on ntfy.
func newNtfyAsyncPublisher() core.NtfyPublishFunc {
	return func(_ context.Context, settings core.NtfySettings, msg core.NtfyMessage) error {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := core.PublishNtfy(ctx, settings, msg); err != nil {
				// SECURITY: log the error only. core.PublishNtfy guarantees its
				// error text never contains the token or password. Never log settings.
				log.Printf("ntfy publish failed: %v", err)
			}
		}()
		return nil
	}
}

// initNtfyNotifier builds s.ntfyNotifier so handlers and tests always have a
// non-nil notifier to call into, regardless of whether startNtfyNotifier ever
// starts the background poller/sampler hook.
func (s *Server) initNtfyNotifier() {
	s.ntfyNotifier = core.NewNtfyNotifier(
		func(ctx context.Context) (core.NtfySettings, error) { return s.store.GetNtfySettings(ctx) },
		newNtfyAsyncPublisher(),
	)
	// Icon/Click links use the same public domain configured for subscription
	// delivery (Settings > Subscription Domain) — read live so an operator
	// edit takes effect without a restart, same as ntfy settings themselves.
	// Empty domain means Icon/Click are simply omitted (see NtfyNotifier.send).
	s.ntfyNotifier.SetBaseURL(func() string {
		domain := strings.TrimSpace(s.config.SubscriptionDomain)
		if domain == "" {
			return ""
		}
		if strings.Contains(domain, "://") {
			return domain
		}
		return "https://" + domain
	})
}

// startNtfyNotifier starts the dedicated background status poller (NTFY-01)
// and installs the traffic-threshold hook on the stats sampler cadence
// (NTFY-02). It is a no-op in demo mode and when neither sing-box nor
// WireGuard is enabled.
func (s *Server) startNtfyNotifier() {
	if s.config.DemoMode || (!s.config.EnableSingbox && !s.config.EnableWireGuard) {
		return
	}

	go func() {
		ticker := time.NewTicker(ntfyStatusPollInterval)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), ntfyPollTimeout)
			s.pollNtfyServiceStatusOnce(ctx)
			cancel()
		}
	}()

	if s.sampler != nil {
		s.sampler.SetOnSampled(s.checkNtfyTrafficThreshold)
	}
}

// pollNtfyServiceStatusOnce checks each enabled service's live status and
// feeds it to the notifier's transition detection. This is the only caller
// of ObserveServiceStatus; handleGetSystemStatus (browser polling) never
// triggers notifications (research Pitfall 1).
func (s *Server) pollNtfyServiceStatusOnce(ctx context.Context) {
	if s.config.EnableSingbox {
		s.observeServiceForNtfy(ctx, core.NtfyServiceSingbox, "sing-box")
	}
	if s.config.EnableWireGuard {
		s.observeServiceForNtfy(ctx, core.NtfyServiceWireGuard, "wireguard")
	}
}

// observeServiceForNtfy feeds one confirmed observation to the notifier. A
// failed check is skipped, and "down" is only reported if it holds on re-check.
func (s *Server) observeServiceForNtfy(ctx context.Context, notifierService, unit string) {
	active, known := s.serviceActive(ctx, unit)
	if !known {
		return
	}
	for i := 0; !active && i < ntfyDownRechecks; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(ntfyDownRecheckDelay):
		}
		active, known = s.serviceActive(ctx, unit)
		if !known {
			return
		}
	}
	s.ntfyNotifier.ObserveServiceStatus(ctx, notifierService, active)
}

// checkNtfyTrafficThreshold computes total panel traffic over the trailing
// sampler interval and feeds it to the notifier's threshold check (NTFY-02).
// Invoked once per completed stats-sampler pass via StatsSampler.SetOnSampled.
func (s *Server) checkNtfyTrafficThreshold(at time.Time) {
	window := time.Duration(s.config.SamplerIntervalSec) * time.Second
	if window <= 0 {
		window = 120 * time.Second
	}
	total, err := s.store.GetCombinedTrafficTotal(at.Add(-window).Unix(), at.Unix())
	if err != nil {
		log.Printf("ntfy traffic check: %v", err)
		return
	}
	s.ntfyNotifier.ObserveTrafficTotal(context.Background(), total, window)
}
