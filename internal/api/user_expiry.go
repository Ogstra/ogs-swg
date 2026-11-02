package api

import "github.com/Ogstra/ogs-swg/internal/core"

// applyUserExpiry stores a user's expiry. base is what the user had before (or
// the zero value for a new user); fields the request omits keep that value.
func (s *Server) applyUserExpiry(email string, expiresAt, afterDays *int64, base core.UserExpiry) error {
	next := base
	if expiresAt != nil {
		next.ExpiresAt = *expiresAt
	}
	if afterDays != nil {
		next.ExpireAfterDays = *afterDays
	}
	// Nothing set before and nothing now: leave the row alone. Clearing an
	// existing expiry still goes through, because next differs from base.
	if next == (core.UserExpiry{}) && base == (core.UserExpiry{}) {
		return nil
	}
	return s.store.SetUserExpiry(email, next)
}
