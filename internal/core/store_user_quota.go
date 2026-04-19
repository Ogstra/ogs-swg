package core

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
)

func quotaWindowStart(period string, now time.Time) int64 {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "daily":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	case "total", "none":
		return 0
	case "monthly", "":
		fallthrough
	default:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	}
}

func userQuotaUsage(meta *UserMetadata, now time.Time, store *Store) (int64, bool, error) {
	if meta == nil {
		return 0, false, nil
	}
	if meta.QuotaLimit <= 0 {
		return 0, false, nil
	}

	samples, err := store.GetCombinedReport(meta.Email, quotaWindowStart(meta.QuotaPeriod, now), now.Unix())
	if err != nil {
		return 0, false, err
	}

	var used int64
	for _, sample := range samples {
		used += sample.Uplink + sample.Downlink
	}
	return used, used >= meta.QuotaLimit, nil
}

func credentialFromInbound(info UserInboundInfo) string {
	if strings.TrimSpace(info.UUID) != "" {
		return strings.TrimSpace(info.UUID)
	}
	return strings.TrimSpace(info.Password)
}

func normalizeInboundTags(tags ...string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func captureUserRestoreState(meta *UserMetadata, cfg *Config) {
	if meta == nil || cfg == nil {
		return
	}
	inbounds, err := cfg.GetUserInbounds(meta.Email)
	if err != nil || len(inbounds) == 0 {
		return
	}

	meta.InboundTags = normalizeInboundTags(inbounds[0].Tag)
	credential := credentialFromInbound(inbounds[0])
	if credential != "" {
		meta.Credential = credential
	}
	if inbounds[0].Flow != "" {
		meta.Flow = inbounds[0].Flow
	}
	if inbounds[0].VmessSecurity != "" {
		meta.VmessSecurity = inbounds[0].VmessSecurity
	}
	if inbounds[0].VmessAlterID != 0 {
		meta.VmessAlterID = inbounds[0].VmessAlterID
	}
}

func (s *Store) restoreUserFromMetadata(meta *UserMetadata, cfg *Config) error {
	if meta == nil {
		return fmt.Errorf("user metadata is required")
	}
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if len(meta.InboundTags) == 0 {
		return fmt.Errorf("user %s has no stored inbound tags", meta.Email)
	}
	if strings.TrimSpace(meta.Credential) == "" {
		return fmt.Errorf("user %s has no stored credential", meta.Email)
	}

	tag := meta.InboundTags[0]
	if err := cfg.UpdateUserInInbound(meta.Email, meta.Credential, meta.Flow, tag, meta.VmessSecurity, meta.VmessAlterID); err == nil {
		return nil
	}
	if err := cfg.AddUser(meta.Email, meta.Credential, meta.Flow, tag, meta.VmessSecurity, meta.VmessAlterID); err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

func (s *Store) userStillOverSubscriptionQuota(email string, now time.Time) bool {
	subs, err := s.Queries.GetSubscriptionsForUser(context.Background(), email)
	if err != nil {
		log.Printf("userStillOverSubscriptionQuota: subscriptions for %s: %v", email, err)
		return false
	}
	for _, sub := range subs {
		limit := sub.QuotaLimit.Int64
		if limit <= 0 {
			continue
		}
		used, err := s.subscriptionUsage(sub.ID, sub.QuotaPeriod.String, now)
		if err != nil {
			log.Printf("userStillOverSubscriptionQuota: usage for %s sub %d: %v", email, sub.ID, err)
			continue
		}
		if used >= limit {
			return true
		}
	}
	return false
}

// EnforceUserQuotas evaluates all users with individual quota metadata and
// bidirectionally enforces them against the live sing-box config.
func (s *Store) EnforceUserQuotas(cfg *Config) {
	ctx := context.Background()

	users, err := s.Queries.GetAllUsers(ctx)
	if err != nil {
		log.Printf("EnforceUserQuotas: GetAllUsers error: %v", err)
		return
	}

	now := time.Now()

	for _, row := range users {
		meta, err := s.GetUserMetadata(row.Email)
		if err != nil || meta == nil {
			continue
		}

		used, overQuota, err := userQuotaUsage(meta, now, s)
		if err != nil {
			log.Printf("EnforceUserQuotas: usage query for %s: %v", meta.Email, err)
			continue
		}

		if overQuota {
			if meta.Enabled {
				captureUserRestoreState(meta, cfg)
				meta.Enabled = false
				_ = cfg.RemoveUser(meta.Email)
				if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
					log.Printf("EnforceUserQuotas: disable %s: %v", meta.Email, saveErr)
				} else {
					log.Printf("EnforceUserQuotas: disabled %s (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
				}
			}
			continue
		}

		if !meta.Enabled {
			if err := s.restoreUserFromMetadata(meta, cfg); err != nil {
				log.Printf("EnforceUserQuotas: restore %s: %v", meta.Email, err)
				continue
			}
			meta.Enabled = true
			if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
				log.Printf("EnforceUserQuotas: re-enable %s metadata: %v", meta.Email, saveErr)
			} else {
				log.Printf("EnforceUserQuotas: re-enabled %s (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
			}
		}
	}
}

// EnforceUserQuotaNow re-evaluates a single user immediately after an edit.
// It only disables when the user is currently over quota; re-enable remains
// owned by the periodic sampler flow so multi-request edit sequences do not race.
func (s *Store) EnforceUserQuotaNow(email string, cfg *Config) error {
	meta, err := s.GetUserMetadata(email)
	if err != nil || meta == nil {
		return err
	}

	used, overQuota, err := userQuotaUsage(meta, time.Now(), s)
	if err != nil {
		return err
	}
	if !overQuota || !meta.Enabled {
		return nil
	}

	captureUserRestoreState(meta, cfg)
	meta.Enabled = false
	_ = cfg.RemoveUser(meta.Email)
	if err := s.SaveUserMetadata(*meta); err != nil {
		return err
	}

	log.Printf("EnforceUserQuotaNow: disabled %s immediately after edit (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
	return nil
}

func (s *Store) ReconcileUserQuotaNow(email string, cfg *Config) error {
	meta, err := s.GetUserMetadata(email)
	if err != nil || meta == nil {
		return err
	}

	now := time.Now()
	used, overOwnQuota, err := userQuotaUsage(meta, now, s)
	if err != nil {
		return err
	}
	overSubscriptionQuota := s.userStillOverSubscriptionQuota(email, now)
	inbounds, err := cfg.GetUserInbounds(meta.Email)
	if err != nil {
		return err
	}
	isActive := len(inbounds) > 0

	if overOwnQuota || overSubscriptionQuota {
		if isActive {
			captureUserRestoreState(meta, cfg)
			_ = cfg.RemoveUser(meta.Email)
		}
		if meta.Enabled || isActive {
			meta.Enabled = false
			if err := s.SaveUserMetadata(*meta); err != nil {
				return err
			}
			log.Printf("ReconcileUserQuotaNow: disabled %s immediately after edit (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
		}
		return nil
	}

	if !isActive {
		if err := s.restoreUserFromMetadata(meta, cfg); err != nil {
			return err
		}
	}
	if !meta.Enabled || !isActive {
		meta.Enabled = true
		if err := s.SaveUserMetadata(*meta); err != nil {
			return err
		}
		log.Printf("ReconcileUserQuotaNow: enabled %s immediately after edit (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
	}
	return nil
}

func (s *Store) userStillOverOwnQuota(meta *UserMetadata, now time.Time) bool {
	_, overQuota, err := userQuotaUsage(meta, now, s)
	if err != nil {
		log.Printf("userStillOverOwnQuota: usage query for %s: %v", meta.Email, err)
		return false
	}
	return overQuota
}

func subscriptionUsageWindowStart(period string, now time.Time) int64 {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "daily":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	case "total", "none":
		return 0
	case "monthly", "":
		fallthrough
	default:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	}
}

func (s *Store) subscriptionUsage(subID int64, quotaPeriod string, now time.Time) (int64, error) {
	return s.Queries.GetSubscriptionUsageInRange(context.Background(), sqlcStore.GetSubscriptionUsageInRangeParams{
		SubID: subID,
		Ts:    subscriptionUsageWindowStart(quotaPeriod, now),
		Ts_2:  now.Unix(),
	})
}
