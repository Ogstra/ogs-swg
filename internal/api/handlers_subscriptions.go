package api

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

// generateToken creates a random 32-character hex string
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type SubscriptionResponse struct {
	ID                         int64                        `json:"id"`
	Token                      *string                      `json:"token,omitempty"`
	Name                       string                       `json:"name"`
	QuotaLimit                 int64                        `json:"quota_limit"`
	QuotaPeriod                string                       `json:"quota_period"`
	UsedBytes                  int64                        `json:"used_bytes"`
	Users                      []string                     `json:"users"`
	Members                    []SubscriptionMemberResponse `json:"members"`
	ProfileUpdateIntervalHours *int64                       `json:"profile_update_interval_hours"`
	UpdateAlways               bool                         `json:"update_always"`
	LastRequestAt              *int64                       `json:"last_request_at"`
	CreatedAt                  int64                        `json:"created_at"`
	UpdatedAt                  int64                        `json:"updated_at"`
}

type SubscriptionMemberResponse struct {
	Username string `json:"username"`
	Alias    string `json:"alias"`
}

type SubscriptionDefaultsResponse struct {
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	Destinations               []string `json:"destinations"`
}

type UpdateSubscriptionDefaultsRequest struct {
	ProfileUpdateIntervalHours optionalInt64Field `json:"profile_update_interval_hours"`
	UpdateAlways               *bool              `json:"update_always"`
	Destinations               []string           `json:"destinations"`
}

type SubscriptionDefaultDestinationsResponse struct {
	Destinations []string `json:"destinations"`
}

var subscriptionDefaultDestinationLogPattern = regexp.MustCompile(`\[OGS\].*inbound(?: packet)? connection to (\S+)`)

func defaultSubscriptionDefaults() SubscriptionDefaultsResponse {
	return SubscriptionDefaultsResponse{
		ProfileUpdateIntervalHours: nil,
		UpdateAlways:               false,
		Destinations:               []string{},
	}
}

func subscriptionDefaultsResponseFromStore(defaults core.SubscriptionDefaults) SubscriptionDefaultsResponse {
	return SubscriptionDefaultsResponse{
		ProfileUpdateIntervalHours: defaults.ProfileUpdateIntervalHours,
		UpdateAlways:               defaults.UpdateAlways,
		Destinations:               defaults.Destinations,
	}
}

func ensureAuthenticatedPanelUser(r *http.Request) (string, error) {
	username, ok := currentPanelUsername(r)
	if !ok {
		return "", errors.New("panel user token required")
	}
	return username, nil
}

func normalizeSubscriptionDefaultDestinations(destinations []string) ([]string, error) {
	if len(destinations) == 0 {
		return []string{}, nil
	}

	normalized := make([]string, 0, len(destinations))
	seen := make(map[string]struct{}, len(destinations))
	for _, raw := range destinations {
		token, err := normalizeDestinationToken(raw)
		if err != nil {
			return nil, httpError("destinations must contain valid host:port values")
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		normalized = append(normalized, token)
	}
	return normalized, nil
}

func normalizeDestinationToken(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("empty destination")
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", err
	}
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return "", errors.New("missing host")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum <= 0 || portNum > 65535 {
		return "", errors.New("invalid port")
	}
	return net.JoinHostPort(host, strconv.Itoa(portNum)), nil
}

func isLoopbackDestination(destination string) bool {
	host, _, err := net.SplitHostPort(destination)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseSubscriptionDefaultDestinations(lines []string, limit int) []string {
	if limit <= 0 {
		limit = 10
	}

	destinations := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for i := len(lines) - 1; i >= 0 && len(destinations) < limit; i-- {
		match := subscriptionDefaultDestinationLogPattern.FindStringSubmatch(lines[i])
		if len(match) != 2 {
			continue
		}
		token, err := normalizeDestinationToken(match[1])
		if err != nil || isLoopbackDestination(token) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		destinations = append(destinations, token)
	}
	return destinations
}

func (s *Server) handleGetSubscriptionDefaults(w http.ResponseWriter, r *http.Request) {
	username, err := ensureAuthenticatedPanelUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	defaults, err := s.store.GetPanelUserSubscriptionDefaults(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Failed to get subscription defaults", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subscriptionDefaultsResponseFromStore(defaults))
}

func (s *Server) handleUpdateSubscriptionDefaults(w http.ResponseWriter, r *http.Request) {
	username, err := ensureAuthenticatedPanelUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req UpdateSubscriptionDefaultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if err := validateSubscriptionRefreshPolicy(req.ProfileUpdateIntervalHours.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	destinations, err := normalizeSubscriptionDefaultDestinations(req.Destinations)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	defaults := core.SubscriptionDefaults{
		ProfileUpdateIntervalHours: req.ProfileUpdateIntervalHours.Value,
		UpdateAlways:               req.UpdateAlways != nil && *req.UpdateAlways,
		Destinations:               destinations,
	}
	if err := s.store.UpdatePanelUserSubscriptionDefaults(r.Context(), username, defaults); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Failed to update subscription defaults", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subscriptionDefaultsResponseFromStore(defaults))
}

func (s *Server) handleGetSubscriptionDefaultDestinations(w http.ResponseWriter, r *http.Request) {
	if _, err := ensureAuthenticatedPanelUser(r); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lines, err := s.readAllSearchableLogLines(r.Context())
	if err != nil {
		lines = nil
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SubscriptionDefaultDestinationsResponse{
		Destinations: parseSubscriptionDefaultDestinations(lines, 10),
	})
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
	includeSecrets := canManageSubscriptionSecrets(r)
	for _, sub := range subs {
		memberRows, _ := s.store.Queries.GetSubscriptionMembers(r.Context(), sub.ID)
		members := subscriptionMembersResponseFromStore(memberRows)
		usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
			SubID: sub.ID,
			Ts:    startOfMonth.Unix(),
			Ts_2:  now.Unix(),
		})
		var token *string
		if includeSecrets {
			token = &sub.Token
		}
		res = append(res, SubscriptionResponse{
			ID:                         sub.ID,
			Token:                      token,
			Name:                       sub.Name,
			QuotaLimit:                 sub.QuotaLimit.Int64,
			QuotaPeriod:                sub.QuotaPeriod.String,
			UsedBytes:                  usedBytes,
			Users:                      subscriptionUsersFromMembers(members),
			Members:                    members,
			ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
			UpdateAlways:               int64ToBool(sub.UpdateAlways),
			LastRequestAt:              nullableInt64Ptr(sub.LastRequestAt),
			CreatedAt:                  sub.CreatedAt.Int64,
			UpdatedAt:                  sub.UpdatedAt.Int64,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type CreateSubscriptionRequest struct {
	Name                       string                      `json:"name"`
	QuotaLimit                 int64                       `json:"quota_limit"`
	QuotaPeriod                string                      `json:"quota_period"`
	Users                      []string                    `json:"users"`
	Members                    []SubscriptionMemberRequest `json:"members"`
	ProfileUpdateIntervalHours optionalInt64Field          `json:"profile_update_interval_hours"`
	UpdateAlways               *bool                       `json:"update_always"`
}

type SubscriptionMemberRequest struct {
	Username string `json:"username"`
	Alias    string `json:"alias"`
}

func normalizeSubscriptionMembers(users []string, members []SubscriptionMemberRequest) []SubscriptionMemberRequest {
	normalized := make([]SubscriptionMemberRequest, 0, len(users)+len(members))
	seen := make(map[string]struct{}, len(users)+len(members))

	appendMember := func(username, alias string) {
		trimmedUsername := strings.TrimSpace(username)
		if trimmedUsername == "" {
			return
		}
		if _, ok := seen[trimmedUsername]; ok {
			return
		}

		seen[trimmedUsername] = struct{}{}
		normalized = append(normalized, SubscriptionMemberRequest{
			Username: trimmedUsername,
			Alias:    strings.TrimSpace(alias),
		})
	}

	if len(members) > 0 {
		for _, member := range members {
			appendMember(member.Username, member.Alias)
		}
		return normalized
	}

	for _, username := range users {
		appendMember(username, "")
	}
	return normalized
}

func subscriptionMembersResponseFromStore(rows []store.SubscriptionUser) []SubscriptionMemberResponse {
	members := make([]SubscriptionMemberResponse, 0, len(rows))
	for _, row := range rows {
		members = append(members, SubscriptionMemberResponse{
			Username: row.UserName,
			Alias:    row.Alias,
		})
	}
	return members
}

func subscriptionUsersFromMembers(members []SubscriptionMemberResponse) []string {
	users := make([]string, 0, len(members))
	for _, member := range members {
		users = append(users, member.Username)
	}
	return users
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
	if err := validateSubscriptionRefreshPolicy(req.ProfileUpdateIntervalHours.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	members := normalizeSubscriptionMembers(req.Users, req.Members)

	token := generateToken()
	id, err := s.store.Queries.CreateSubscription(r.Context(), store.CreateSubscriptionParams{
		Token:                      token,
		Name:                       req.Name,
		QuotaLimit:                 sql.NullInt64{Int64: req.QuotaLimit, Valid: true},
		QuotaPeriod:                sql.NullString{String: req.QuotaPeriod, Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: nullableInt64(req.ProfileUpdateIntervalHours.Value),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, false),
	})
	if err != nil {
		http.Error(w, "Failed to create subscription: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, member := range members {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: member.Username,
			Alias:    member.Alias,
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

	memberRows, _ := s.store.Queries.GetSubscriptionMembers(r.Context(), id)
	members := subscriptionMembersResponseFromStore(memberRows)

	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
		SubID: id,
		Ts:    startOfMonth.Unix(),
		Ts_2:  now.Unix(),
	})
	var token *string
	if canManageSubscriptionSecrets(r) {
		token = &sub.Token
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SubscriptionResponse{
		ID:                         sub.ID,
		Token:                      token,
		Name:                       sub.Name,
		QuotaLimit:                 sub.QuotaLimit.Int64,
		QuotaPeriod:                sub.QuotaPeriod.String,
		UsedBytes:                  usedBytes,
		Users:                      subscriptionUsersFromMembers(members),
		Members:                    members,
		ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		UpdateAlways:               int64ToBool(sub.UpdateAlways),
		LastRequestAt:              nullableInt64Ptr(sub.LastRequestAt),
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
	if err := validateSubscriptionRefreshPolicy(req.ProfileUpdateIntervalHours.Value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	members := normalizeSubscriptionMembers(req.Users, req.Members)

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
		ProfileUpdateIntervalHours: mergeNullableInt64(current.ProfileUpdateIntervalHours, req.ProfileUpdateIntervalHours),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, int64ToBool(current.UpdateAlways)),
		ID:                         id,
	})
	if err != nil {
		http.Error(w, "Failed to update subscription", http.StatusInternalServerError)
		return
	}

	s.store.Queries.ClearSubscriptionUsers(r.Context(), id)
	for _, member := range members {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: member.Username,
			Alias:    member.Alias,
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

func canManageSubscriptionSecrets(r *http.Request) bool {
	perms := getPermissions(r)
	if perms == nil {
		return true
	}
	return perms.CanWriteUsers
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
