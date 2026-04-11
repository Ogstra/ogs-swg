package core

import (
	"context"
	"log"
	"time"
)

// EnforceSubscriptionQuotas evaluates all subscriptions with quota_limit > 0 and
// bidirectionally enforces them:
//   - If total period usage >= quota_limit → disable all assigned users in sing-box.
//   - If total period usage < quota_limit  → re-enable disabled users and restore them in sing-box.
func (s *Store) EnforceSubscriptionQuotas(cfg *Config) {
	ctx := context.Background()

	subs, err := s.Queries.GetAllSubscriptions(ctx)
	if err != nil {
		log.Printf("EnforceSubscriptionQuotas: GetAllSubscriptions error: %v", err)
		return
	}

	now := time.Now()

	for _, sub := range subs {
		limit := sub.QuotaLimit.Int64
		if limit <= 0 {
			continue // unlimited subscription
		}

		used, err := s.subscriptionUsage(sub.ID, sub.QuotaPeriod.String, now)
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
					captureUserRestoreState(meta, cfg)
					meta.Enabled = false
					_ = cfg.RemoveUser(userName)
					if saveErr := s.SaveUserMetadata(*meta); saveErr != nil {
						log.Printf("EnforceSubscriptionQuotas: disable %s: %v", userName, saveErr)
					} else {
						log.Printf("EnforceSubscriptionQuotas: disabled %s (sub %d, used=%d, limit=%d)", userName, sub.ID, used, limit)
					}
				}
			} else {
				// Keep users disabled if they are still over their own individual quota.
				if !meta.Enabled {
					if s.userStillOverOwnQuota(meta, now) {
						continue
					}
					if err := s.restoreUserFromMetadata(meta, cfg); err != nil {
						log.Printf("EnforceSubscriptionQuotas: restore %s: %v", userName, err)
						continue
					}
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
