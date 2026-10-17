package api

import (
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

var hwidMigrationNoProviderLogged atomic.Bool

// pendingHWIDMigrationURL returns the new-url to send to this request's
// device, or "" when none applies. It never fails the request: any error
// means "no migration". It only does work for Happ requests that carry an
// HWID, so every other request pays nothing and its bytes are unchanged.
func (s *Server) pendingHWIDMigrationURL(r *http.Request, sub store.GetSubscriptionByTokenRow) string {
	if s.store == nil || !isHappRequest(r) {
		return ""
	}
	hash := extractSubscriptionRequestMetadata(r).hwidHash
	if hash == "" {
		return ""
	}
	m, err := s.store.GetLiveHWIDMigration(r.Context(), sub.ID, hash)
	if err != nil {
		log.Printf("hwid migration: lookup for sub %d: %v", sub.ID, err)
		return ""
	}
	if m == nil {
		return ""
	}
	cfg, err := s.store.GetSubscriptionHappConfig(r.Context())
	if err != nil {
		log.Printf("hwid migration: happ config: %v", err)
		return ""
	}
	// Happ ignores its advanced params without a provider id.
	if strings.TrimSpace(cfg.ProviderID) == "" {
		if hwidMigrationNoProviderLogged.CompareAndSwap(false, true) {
			log.Printf("hwid migration: providerid is not configured; new-url cannot be delivered")
		}
		return ""
	}
	target, err := s.store.Queries.GetSubscriptionByID(r.Context(), m.TargetSubID)
	if err != nil {
		log.Printf("hwid migration: target sub %d: %v", m.TargetSubID, err)
		return ""
	}
	failed, err := s.store.MarkHWIDMigrationDelivered(r.Context(), sub.ID, hash, s.now().Unix())
	if err != nil {
		log.Printf("hwid migration: mark delivered: %v", err)
	}
	if failed {
		s.insertSystemAuditEntry("subscription", "hwid_migration_failed", sub.Name, "device never reached the target subscription")
		if s.ntfyNotifier != nil {
			s.ntfyNotifier.ObserveHWIDMigration(r.Context(), core.NtfyHWIDMigrationInfo{
				SourceSubscription: sub.Name,
				TargetSubscription: target.Name,
				HWIDPrefix:         extractSubscriptionRequestMetadata(r).hwidPrefix,
				Failed:             true,
			})
		}
	}

	base := ""
	for _, p := range cfg.AdvancedParameters {
		if strings.EqualFold(strings.TrimSpace(p.Key), "new-domain") {
			base = strings.TrimSpace(p.Value)
			break
		}
	}
	return s.migrationSubscriptionURL(r, base, target.Token)
}

// observeHWIDMigrationArrival confirms a migration once its device requests
// the target sub. Same contract as observeSubscriptionHWID: side-effect only,
// failures are logged and swallowed. When it confirms, the HWID is recorded as
// known on the target so the new-device ntfy is not fired for a moved device.
func (s *Server) observeHWIDMigrationArrival(r *http.Request, sub store.GetSubscriptionByTokenRow) {
	if s.store == nil {
		return
	}
	meta := extractSubscriptionRequestMetadata(r)
	if meta.hwidHash == "" {
		return
	}
	confirmed, err := s.store.ConfirmHWIDMigration(r.Context(), sub.ID, meta.hwidHash, s.now().Unix())
	if err != nil {
		log.Printf("hwid migration: confirm for sub %d: %v", sub.ID, err)
		return
	}
	if !confirmed {
		return
	}
	if _, err := s.store.RecordSubscriptionHWIDFirstSeen(r.Context(), sub.ID, meta.hwidHash, s.now().Unix()); err != nil {
		log.Printf("hwid migration: record hwid for sub %d: %v", sub.ID, err)
	}
	s.insertSystemAuditEntry("subscription", "hwid_migration_confirmed", sub.Name, "device "+meta.hwidPrefix+" reached the target subscription")
	if s.ntfyNotifier != nil {
		s.ntfyNotifier.ObserveHWIDMigration(r.Context(), core.NtfyHWIDMigrationInfo{
			TargetSubscription: sub.Name,
			HWIDPrefix:         meta.hwidPrefix,
		})
	}
}

// migrationSubscriptionURL builds the link the CLIENT must use: the global
// new-domain if set, else the configured subscription domain, else the
// request's public host. It is never derived from the global fallback-url,
// which points at the origin rather than the host clients go through.
func (s *Server) migrationSubscriptionURL(r *http.Request, newDomain, token string) string {
	base := newDomain
	if base == "" {
		base = strings.TrimSpace(s.config.SubscriptionDomain)
	}
	if base == "" {
		base = s.resolvePublicHost(r)
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return strings.TrimRight(base, "/") + "/s/" + token
}
