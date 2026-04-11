package core

import (
	"context"
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
			meta.Enabled = true
			if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
				log.Printf("EnforceUserQuotas: re-enable %s metadata: %v", meta.Email, saveErr)
			} else {
				log.Printf("EnforceUserQuotas: re-enabled %s (used=%d, limit=%d, period=%s)", meta.Email, used, meta.QuotaLimit, meta.QuotaPeriod)
			}
		}
	}
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
		Ts2:   now.Unix(),
	})
}
