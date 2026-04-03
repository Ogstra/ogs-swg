package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// generateToken creates a random 32-character hex string
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type SubscriptionResponse struct {
	ID                         int64    `json:"id"`
	Token                      string   `json:"token"`
	Name                       string   `json:"name"`
	QuotaLimit                 int64    `json:"quota_limit"`
	QuotaPeriod                string   `json:"quota_period"`
	UsedBytes                  int64    `json:"used_bytes"`
	Users                      []string `json:"users"`
	ExpiresAt                  *int64   `json:"expires_at"`
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	CreatedAt                  int64    `json:"created_at"`
	UpdatedAt                  int64    `json:"updated_at"`
}

func (s *Server) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.Queries.GetAllSubscriptions(r.Context())
	if err != nil {
		http.Error(w, "Failed to get subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	res := make([]SubscriptionResponse, 0, len(subs))
	for _, sub := range subs {
		users, _ := s.store.Queries.GetUsersForSubscription(r.Context(), sub.ID)
		usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
			SubID: sub.ID,
			Ts:    startOfMonth.Unix(),
			Ts2:   now.Unix(),
		})
		res = append(res, SubscriptionResponse{
			ID:                         sub.ID,
			Token:                      sub.Token,
			Name:                       sub.Name,
			QuotaLimit:                 sub.QuotaLimit.Int64,
			QuotaPeriod:                sub.QuotaPeriod.String,
			UsedBytes:                  usedBytes,
			Users:                      users,
			ExpiresAt:                  nullableInt64Ptr(sub.ExpiresAt),
			ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
			UpdateAlways:               int64ToBool(sub.UpdateAlways),
			CreatedAt:                  sub.CreatedAt.Int64,
			UpdatedAt:                  sub.UpdatedAt.Int64,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type CreateSubscriptionRequest struct {
	Name                       string             `json:"name"`
	QuotaLimit                 int64              `json:"quota_limit"`
	QuotaPeriod                string             `json:"quota_period"`
	Users                      []string           `json:"users"`
	ExpiresAt                  optionalInt64Field `json:"expires_at"`
	ProfileUpdateIntervalHours optionalInt64Field `json:"profile_update_interval_hours"`
	UpdateAlways               *bool              `json:"update_always"`
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.QuotaPeriod == "" {
		req.QuotaPeriod = "monthly"
	}
	if err := validateSubscriptionExpiration(req.ExpiresAt.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateSubscriptionRefreshPolicy(req.ProfileUpdateIntervalHours.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token := generateToken()
	id, err := s.store.Queries.CreateSubscription(r.Context(), store.CreateSubscriptionParams{
		Token:                      token,
		Name:                       req.Name,
		QuotaLimit:                 sql.NullInt64{Int64: req.QuotaLimit, Valid: true},
		QuotaPeriod:                sql.NullString{String: req.QuotaPeriod, Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ExpiresAt:                  nullableInt64(req.ExpiresAt.Value),
		ProfileUpdateIntervalHours: nullableInt64(req.ProfileUpdateIntervalHours.Value),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, false),
	})
	if err != nil {
		http.Error(w, "Failed to create subscription: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, user := range req.Users {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: user,
		})
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "token": token})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	sub, err := s.store.Queries.GetSubscriptionByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	users, _ := s.store.Queries.GetUsersForSubscription(r.Context(), id)

	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
		SubID: id,
		Ts:    startOfMonth.Unix(),
		Ts2:   now.Unix(),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SubscriptionResponse{
		ID:                         sub.ID,
		Token:                      sub.Token,
		Name:                       sub.Name,
		QuotaLimit:                 sub.QuotaLimit.Int64,
		QuotaPeriod:                sub.QuotaPeriod.String,
		UsedBytes:                  usedBytes,
		Users:                      users,
		ExpiresAt:                  nullableInt64Ptr(sub.ExpiresAt),
		ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		UpdateAlways:               int64ToBool(sub.UpdateAlways),
		CreatedAt:                  sub.CreatedAt.Int64,
		UpdatedAt:                  sub.UpdatedAt.Int64,
	})
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.QuotaPeriod == "" {
		req.QuotaPeriod = "monthly"
	}
	if err := validateSubscriptionExpiration(req.ExpiresAt.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateSubscriptionRefreshPolicy(req.ProfileUpdateIntervalHours.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	current, err := s.store.Queries.GetSubscriptionByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	err = s.store.Queries.UpdateSubscription(r.Context(), store.UpdateSubscriptionParams{
		Name:                       req.Name,
		QuotaLimit:                 sql.NullInt64{Int64: req.QuotaLimit, Valid: true},
		QuotaPeriod:                sql.NullString{String: req.QuotaPeriod, Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ExpiresAt:                  mergeNullableInt64(current.ExpiresAt, req.ExpiresAt),
		ProfileUpdateIntervalHours: mergeNullableInt64(current.ProfileUpdateIntervalHours, req.ProfileUpdateIntervalHours),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, int64ToBool(current.UpdateAlways)),
		ID:                         id,
	})
	if err != nil {
		http.Error(w, "Failed to update subscription", http.StatusInternalServerError)
		return
	}

	s.store.Queries.ClearSubscriptionUsers(r.Context(), id)
	for _, user := range req.Users {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: user,
		})
	}

	// Re-evaluate quota state after update (quota may have been raised, triggering re-enable)
	go s.store.EnforceSubscriptionQuotas(s.config)

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func validateSubscriptionRefreshPolicy(interval *int64) error {
	if interval != nil && *interval <= 0 {
		return httpError("profile_update_interval_hours must be greater than zero")
	}
	return nil
}

func validateSubscriptionExpiration(expire *int64) error {
	if expire != nil && *expire <= 0 {
		return httpError("expires_at must be greater than zero")
	}
	return nil
}

type optionalInt64Field struct {
	Set   bool
	Value *int64
}

func (f *optionalInt64Field) UnmarshalJSON(data []byte) error {
	f.Set = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}

	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	f.Value = &value
	return nil
}

func nullableInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func mergeNullableInt64(current sql.NullInt64, next optionalInt64Field) sql.NullInt64 {
	if !next.Set {
		return current
	}
	return nullableInt64(next.Value)
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullableInt64Value(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func boolPtrToInt64(value *bool, fallback bool) int64 {
	if value == nil {
		if fallback {
			return 1
		}
		return 0
	}
	if *value {
		return 1
	}
	return 0
}

func int64ToBool(value int64) bool {
	return value != 0
}

type subscriptionValidationError string

func (e subscriptionValidationError) Error() string {
	return string(e)
}

func httpError(message string) error {
	return subscriptionValidationError(message)
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	err = s.store.Queries.DeleteSubscription(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to delete subscription", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRegenerateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	token := generateToken()
	err = s.store.Queries.RegenerateSubscriptionToken(r.Context(), store.RegenerateSubscriptionTokenParams{
		Token: token,
		ID:    id,
	})
	if err != nil {
		http.Error(w, "Failed to regenerate token", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}
