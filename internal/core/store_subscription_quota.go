package core

import (
	"context"
	"log"
	"time"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
)

// EnforceSubscriptionQuotas evaluates all subscriptions with quota_limit > 0 and
// bidirectionally enforces them:
//   - If total period usage >= quota_limit → disable all assigned users in sing-box.
//   - If total period usage < quota_limit  → re-enable disabled users (e.g. after quota raised).
//
// Re-enabling only updates the metadata DB flag; the next API call or config reload
// will restore the user in sing-box. This matches the behaviour of individual user quotas.
func (s *Store) EnforceSubscriptionQuotas(cfg *Config) {
	ctx := context.Background()

	subs, err := s.Queries.GetAllSubscriptions(ctx)
	if err != nil {
		log.Printf("EnforceSubscriptionQuotas: GetAllSubscriptions error: %v", err)
		return
	}

	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	for _, sub := range subs {
		limit := sub.QuotaLimit.Int64
		if limit <= 0 {
			continue // unlimited subscription
		}

		used, err := s.Queries.GetSubscriptionUsageInRange(ctx, sqlcStore.GetSubscriptionUsageInRangeParams{
			SubID: sub.ID,
			Ts:    startOfMonth.Unix(),
			Ts2:   now.Unix(),
		})
		if err != nil {
			log.Printf("EnforceSubscriptionQuotas: usage query for sub %d: %v", sub.ID, err)
			continue
		}

		users, err := s.Queries.GetUsersForSubscription(ctx, sub.ID)
		if err != nil {
			log.Printf("EnforceSubscriptionQuotas: GetUsersForSubscription %d: %v", sub.ID, err)
			continue
		}

		overQuota := used >= limit

		for _, userName := range users {
			meta, err := s.GetUserMetadata(userName)
			if err != nil || meta == nil {
				continue
			}

			if overQuota {
				if meta.Enabled {
					meta.Enabled = false
					_ = cfg.RemoveUser(userName)
					if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
						log.Printf("EnforceSubscriptionQuotas: disable %s: %v", userName, saveErr)
					} else {
						log.Printf("EnforceSubscriptionQuotas: disabled %s (sub %d, used=%d, limit=%d)", userName, sub.ID, used, limit)
					}
				}
			} else {
				// Quota raised: re-enable user in metadata so next API call/config reload restores them.
				if !meta.Enabled {
					meta.Enabled = true
					if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
						log.Printf("EnforceSubscriptionQuotas: re-enable %s metadata: %v", userName, saveErr)
					} else {
						log.Printf("EnforceSubscriptionQuotas: re-enabled %s (sub %d, used=%d, limit=%d)", userName, sub.ID, used, limit)
					}
				}
			}
		}
	}
}
