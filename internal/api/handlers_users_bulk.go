package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

const (
	maxBulkUserActionNames = 500
	maxBulkExtendDays      = 3650
)

type bulkUserActionRequest struct {
	Names  []string `json:"names"`
	Action string   `json:"action"` // extend_days, add_quota, set_quota, set_expiry, delete
	// Days is added to the expiry (negative removes days).
	Days int64 `json:"days"`
	// QuotaBytes is added to (add_quota) or becomes (set_quota) the quota.
	QuotaBytes int64 `json:"quota_bytes"`
	// ExpiresAt (unix seconds) is the new fixed expiry; 0 clears it.
	ExpiresAt *int64 `json:"expires_at"`
}

type bulkUserActionResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type bulkUserActionResponse struct {
	Results []bulkUserActionResult `json:"results"`
	Done    int                    `json:"done"`
	Failed  int                    `json:"failed"`
}

// extendUserExpiry moves a user's expiry by days. An expired user restarts from
// now, a user counting from first use gets more days, and a user without any
// expiry gets one that many days from now.
func extendUserExpiry(current core.UserExpiry, days int64, now time.Time) (core.UserExpiry, error) {
	next := current
	switch {
	case current.ExpiresAt > 0:
		base := current.ExpiresAt
		if base < now.Unix() {
			base = now.Unix()
		}
		next.ExpiresAt = base + days*86400
		if next.ExpiresAt < now.Unix() {
			next.ExpiresAt = now.Unix()
		}
	case current.ExpireAfterDays > 0:
		next.ExpireAfterDays += days
		if next.ExpireAfterDays < 1 {
			next.ExpireAfterDays = 1
		}
	case days > 0:
		next.ExpiresAt = now.Unix() + days*86400
	default:
		return current, fmt.Errorf("the user has no expiry to shorten")
	}
	return next, nil
}

func (s *Server) handleBulkUserAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var req bulkUserActionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	names := make([]string, 0, len(req.Names))
	seen := map[string]struct{}{}
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if _, dup := seen[name]; name == "" || dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		writeErr(w, http.StatusBadRequest, "Pick at least one user")
		return
	}
	if len(names) > maxBulkUserActionNames {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("At most %d users at a time", maxBulkUserActionNames))
		return
	}
	switch req.Action {
	case "extend_days":
		if req.Days == 0 || req.Days < -maxBulkExtendDays || req.Days > maxBulkExtendDays {
			writeErr(w, http.StatusBadRequest, "days must be between -3650 and 3650, and not 0")
			return
		}
	case "add_quota":
		if req.QuotaBytes <= 0 {
			writeErr(w, http.StatusBadRequest, "quota_bytes must be greater than 0")
			return
		}
	case "set_quota":
		if req.QuotaBytes < 0 {
			writeErr(w, http.StatusBadRequest, "quota_bytes cannot be negative")
			return
		}
	case "set_expiry":
		if req.ExpiresAt == nil || *req.ExpiresAt < 0 {
			writeErr(w, http.StatusBadRequest, "expires_at is required (0 clears it)")
			return
		}
	case "delete":
	default:
		writeErr(w, http.StatusBadRequest, "Unknown action")
		return
	}

	now := time.Now()
	resp := bulkUserActionResponse{Results: make([]bulkUserActionResult, 0, len(names))}
	for _, name := range names {
		err := s.applyBulkUserAction(name, req, now)
		result := bulkUserActionResult{Name: name, OK: err == nil}
		if err != nil {
			result.Error = err.Error()
			resp.Failed++
		} else {
			resp.Done++
		}
		resp.Results = append(resp.Results, result)
	}

	s.cache.Del("api:status")
	s.cache.Del(cacheKeyAllUsers)
	s.InvalidateSubCache()
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) applyBulkUserAction(name string, req bulkUserActionRequest, now time.Time) error {
	if req.Action == "delete" {
		return s.deleteUserEverywhere(name)
	}

	meta, err := s.store.GetUserMetadata(name)
	if err != nil || meta == nil {
		return fmt.Errorf("user not found")
	}
	expiry, err := s.store.GetUserExpiry(name)
	if err != nil {
		return err
	}

	switch req.Action {
	case "extend_days":
		if expiry, err = extendUserExpiry(expiry, req.Days, now); err != nil {
			return err
		}
	case "add_quota":
		if meta.QuotaLimit <= 0 {
			return fmt.Errorf("the quota is unlimited")
		}
		meta.QuotaLimit += req.QuotaBytes
	case "set_quota":
		meta.QuotaLimit = req.QuotaBytes
	case "set_expiry":
		expiry = core.UserExpiry{ExpiresAt: *req.ExpiresAt}
	}

	if err := s.store.SaveUserMetadata(*meta); err != nil {
		return err
	}
	if err := s.store.SetUserExpiry(name, expiry); err != nil {
		return err
	}
	// Disables the user if the change leaves them over, and brings them back if
	// it lifted the limit.
	return s.store.ReconcileUserQuotaNow(name, s.config)
}
