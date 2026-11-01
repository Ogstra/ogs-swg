package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"net/http"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// deviceLimitNotifyEvery keeps a device that retries all day from flooding ntfy.
const deviceLimitNotifyEvery = time.Hour

type deviceLimitNotices struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (n *deviceLimitNotices) shouldNotify(key string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.last == nil {
		n.last = map[string]time.Time{}
	}
	if at, ok := n.last[key]; ok && now.Sub(at) < deviceLimitNotifyEvery {
		return false
	}
	n.last[key] = now
	return true
}

// effectiveDeviceLimit is the subscription's own limit, else the panel default.
func (s *Server) effectiveDeviceLimit(ctx context.Context, sub store.GetSubscriptionByTokenRow) int64 {
	if limit, err := s.store.GetSubscriptionDeviceLimit(ctx, sub.ID); err == nil && limit > 0 {
		return limit
	}
	return int64(s.config.SubscriptionProtection.DefaultDeviceLimit)
}

// checkDeviceLimit decides whether this request may be served. A device that
// is known, has no HWID or is being moved here is never refused. A new device
// is refused once the subscription holds as many as it may; otherwise it is
// recorded right away, under a lock, and admitted reports that so the caller
// does not record it twice.
func (s *Server) checkDeviceLimit(r *http.Request, sub store.GetSubscriptionByTokenRow) (blocked, admitted bool) {
	limit := s.effectiveDeviceLimit(r.Context(), sub)
	if limit <= 0 {
		return false, false
	}
	meta := extractSubscriptionRequestMetadata(r)
	if meta.hwidHash == "" {
		return false, false
	}

	s.deviceLimitMu.Lock()
	defer s.deviceLimitMu.Unlock()

	known, err := s.store.HasSubscriptionKnownHWID(r.Context(), sub.ID, meta.hwidHash)
	if err != nil || known {
		return false, false
	}
	if incoming, err := s.store.HasIncomingHWIDMigration(r.Context(), sub.ID, meta.hwidHash); err != nil || incoming {
		return false, false
	}
	count, err := s.store.CountSubscriptionKnownHWIDs(r.Context(), sub.ID)
	if err != nil {
		return false, false
	}
	if count >= limit {
		return true, false
	}
	isNew, err := s.store.RecordSubscriptionHWIDFirstSeen(r.Context(), sub.ID, meta.hwidHash, s.now().Unix())
	return false, err == nil && isNew
}

func (s *Server) notifyDeviceLimit(r *http.Request, sub store.GetSubscriptionByTokenRow, users []string) {
	if s.ntfyNotifier == nil {
		return
	}
	meta := extractSubscriptionRequestMetadata(r)
	if !s.deviceLimitNotices.shouldNotify(sub.Name+"\x00"+meta.hwidHash, s.now()) {
		return
	}
	s.ntfyNotifier.ObserveDeviceLimitBlocked(r.Context(), core.NtfyDeviceLimitInfo{
		SubscriptionName: sub.Name,
		Username:         strings.Join(users, ", "),
		HWIDPrefix:       meta.hwidPrefix,
		DeviceModel:      meta.deviceModel,
		DeviceOS:         meta.deviceOS,
		DeviceOSVersion:  meta.deviceOSVersion,
	})
}
