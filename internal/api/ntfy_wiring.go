package api

import (
	"context"
	"log"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// ntfyStatusPollInterval is the dedicated background poll cadence used to
// detect sing-box/WireGuard service up/down transitions (NTFY-01). It is
// intentionally decoupled from any browser-facing status handler so that
// dashboard polling can never cause duplicate or missed notifications.
const ntfyStatusPollInterval = 60 * time.Second

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
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		s.ntfyNotifier.ObserveServiceStatus(ctx, core.NtfyServiceSingbox, s.checkService(ctx, "sing-box"))
	}
	if s.config.EnableWireGuard {
		s.ntfyNotifier.ObserveServiceStatus(ctx, core.NtfyServiceWireGuard, s.checkService(ctx, "wireguard"))
	}
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
