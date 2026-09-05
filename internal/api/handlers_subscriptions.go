package api

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	Alias                      string                       `json:"alias"`
	QuotaLimit                 int64                        `json:"quota_limit"`
	QuotaPeriod                string                       `json:"quota_period"`
	UsedBytes                  int64                        `json:"used_bytes"`
	Users                      []string                     `json:"users"`
	Members                    []SubscriptionMemberResponse `json:"members"`
	ProfileUpdateIntervalHours *int64                       `json:"profile_update_interval_hours"`
	UpdateAlways               bool                         `json:"update_always"`
	HappRoutingProfile         string                       `json:"happ_routing_profile"`
	HappColorProfile           string                       `json:"happ_color_profile"`
	HappDirectSites            string                       `json:"happ_direct_sites"`
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

type SubscriptionHappConfigRequest struct {
	ProviderID         string                             `json:"provider_id"`
	HideSettings       string                             `json:"hide_settings"`
	AlwaysHWID         string                             `json:"subscription_always_hwid_enable"`
	AutoUpdateOnOpen   string                             `json:"subscription_auto_update_open_enable"`
	PingOnOpen         string                             `json:"subscription_ping_onopen_enabled"`
	ColorProfile       string                             `json:"color_profile"`
	ProfileFlag        string                             `json:"profile_flag"`
	RoutingProfile     string                             `json:"routing_profile"`
	AdvancedParameters []SubscriptionHappParameterRequest `json:"advanced_parameters"`
	DirectSites        []string                           `json:"direct_sites"`
}

type SubscriptionHappParameterRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
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

	var lines []string
	if s.logStore != nil {
		_ = s.logStore.WalkHot(r.Context(), "[OGS] inbound", 0, 0, func(row core.LogRow) error {
			lines = append(lines, row.Raw)
			if len(lines) >= 200 {
				return errStopLogWalk
			}
			return nil
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SubscriptionDefaultDestinationsResponse{
		Destinations: parseSubscriptionDefaultDestinations(lines, 10),
	})
}

func (s *Server) handleGetSubscriptionHappConfig(w http.ResponseWriter, r *http.Request) {
	if cached, found := s.cache.Get(cacheKeyHappConfig); found {
		if b, ok := cached.([]byte); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
	}

	config, err := s.store.GetSubscriptionHappConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to get Happ config", http.StatusInternalServerError)
		return
	}
	b, err := json.Marshal(config)
	if err != nil {
		http.Error(w, "Failed to encode Happ config", http.StatusInternalServerError)
		return
	}
	s.cache.SetWithTTL(cacheKeyHappConfig, b, int64(len(b)), 60*time.Second)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (s *Server) handleUpdateSubscriptionHappConfig(w http.ResponseWriter, r *http.Request) {
	var req SubscriptionHappConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	config, err := normalizeSubscriptionHappConfig(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.UpdateSubscriptionHappConfig(r.Context(), config); err != nil {
		http.Error(w, "Failed to update Happ config", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	s.cache.Del(cacheKeyHappConfig)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func normalizeSubscriptionHappConfig(req SubscriptionHappConfigRequest) (core.SubscriptionHappConfig, error) {
	hideSettings := strings.TrimSpace(req.HideSettings)
	if hideSettings != "" && hideSettings != "0" && hideSettings != "1" {
		return core.SubscriptionHappConfig{}, httpError("hide_settings must be empty, 0, or 1")
	}
	alwaysHWID := strings.TrimSpace(req.AlwaysHWID)
	if alwaysHWID != "" && alwaysHWID != "0" && alwaysHWID != "1" {
		return core.SubscriptionHappConfig{}, httpError("subscription_always_hwid_enable must be empty, 0, or 1")
	}
	autoUpdateOnOpen := strings.TrimSpace(req.AutoUpdateOnOpen)
	if autoUpdateOnOpen != "" && autoUpdateOnOpen != "0" && autoUpdateOnOpen != "1" {
		return core.SubscriptionHappConfig{}, httpError("subscription_auto_update_open_enable must be empty, 0, or 1")
	}
	pingOnOpen := strings.TrimSpace(req.PingOnOpen)
	if pingOnOpen != "" && pingOnOpen != "0" && pingOnOpen != "1" {
		return core.SubscriptionHappConfig{}, httpError("subscription_ping_onopen_enabled must be empty, 0, or 1")
	}
	colorProfile := normalizeHappSubscriptionParamValue(req.ColorProfile)

	advanced := make([]core.SubscriptionHappParameter, 0, len(req.AdvancedParameters))
	seen := make(map[string]struct{}, len(req.AdvancedParameters))
	for _, param := range req.AdvancedParameters {
		key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(param.Key, "#")))
		value := normalizeHappSubscriptionParamValue(param.Value)
		if key == "" && value == "" {
			continue
		}
		if key == "providerid" || key == "hide-settings" || key == "subscription-always-hwid-enable" || key == "subscription-auto-update-open-enable" || key == "subscription-ping-onopen-enabled" || key == "color-profile" {
			continue
		}
		if !happSubscriptionParamKeyRE.MatchString(key) {
			return core.SubscriptionHappConfig{}, httpError("advanced_parameters contain an invalid Happ parameter name")
		}
		if value == "" {
			return core.SubscriptionHappConfig{}, httpError("advanced_parameters values cannot be empty")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		advanced = append(advanced, core.SubscriptionHappParameter{Key: key, Value: value})
	}

	directSites := make([]string, 0, len(req.DirectSites))
	for _, s := range req.DirectSites {
		s = strings.TrimSpace(s)
		if s != "" {
			directSites = append(directSites, s)
		}
	}

	return core.SubscriptionHappConfig{
		ProviderID:         normalizeHappSubscriptionParamValue(req.ProviderID),
		HideSettings:       hideSettings,
		AlwaysHWID:         alwaysHWID,
		AutoUpdateOnOpen:   autoUpdateOnOpen,
		PingOnOpen:         pingOnOpen,
		ColorProfile:       colorProfile,
		ProfileFlag:        strings.TrimSpace(req.ProfileFlag),
		RoutingProfile:     strings.TrimSpace(req.RoutingProfile),
		AdvancedParameters: advanced,
		DirectSites:        directSites,
	}, nil
}

func (s *Server) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	if cached, found := s.cache.Get(cacheKeyAllSubscriptions); found {
		if b, ok := cached.([]byte); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
	}

	subs, err := s.store.Queries.GetAllSubscriptions(r.Context())
	if err != nil {
		http.Error(w, "Failed to get subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	now := s.now()

	res := make([]SubscriptionResponse, 0, len(subs))
	includeSecrets := canManageSubscriptionSecrets(r)
	for _, sub := range subs {
		memberRows, _ := s.store.Queries.GetSubscriptionMembers(r.Context(), sub.ID)
		members := subscriptionMembersResponseFromStore(memberRows)
		usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
			SubID: sub.ID,
			Ts:    core.SubscriptionUsageWindowStart(sub.QuotaPeriod.String, now),
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
			Alias:                      sub.Alias,
			QuotaLimit:                 sub.QuotaLimit.Int64,
			QuotaPeriod:                sub.QuotaPeriod.String,
			UsedBytes:                  usedBytes,
			Users:                      subscriptionUsersFromMembers(members),
			Members:                    members,
			ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
			UpdateAlways:               int64ToBool(sub.UpdateAlways),
			HappRoutingProfile:         sub.HappRoutingProfile,
			HappColorProfile:           sub.HappColorProfile,
			HappDirectSites:            sub.HappDirectSites,
			LastRequestAt:              interfaceToInt64Ptr(sub.LastRequestAt),
			CreatedAt:                  sub.CreatedAt.Int64,
			UpdatedAt:                  sub.UpdatedAt.Int64,
		})
	}
	b, err := json.Marshal(res)
	if err != nil {
		http.Error(w, "Failed to encode subscriptions", http.StatusInternalServerError)
		return
	}
	s.cache.SetWithTTL(cacheKeyAllSubscriptions, b, int64(len(b)), 15*time.Second)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

type CreateSubscriptionRequest struct {
	Name                       string                      `json:"name"`
	Alias                      string                      `json:"alias"`
	QuotaLimit                 int64                       `json:"quota_limit"`
	QuotaPeriod                string                      `json:"quota_period"`
	Users                      []string                    `json:"users"`
	Members                    []SubscriptionMemberRequest `json:"members"`
	ProfileUpdateIntervalHours optionalInt64Field          `json:"profile_update_interval_hours"`
	UpdateAlways               *bool                       `json:"update_always"`
	HappRoutingProfile         string                      `json:"happ_routing_profile"`
	HappColorProfile           string                      `json:"happ_color_profile"`
	HappDirectSites            string                      `json:"happ_direct_sites"`
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
		Alias:                      strings.TrimSpace(req.Alias),
		QuotaLimit:                 sql.NullInt64{Int64: req.QuotaLimit, Valid: true},
		QuotaPeriod:                sql.NullString{String: req.QuotaPeriod, Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: nullableInt64(req.ProfileUpdateIntervalHours.Value),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, false),
		HappRoutingProfile:         strings.TrimSpace(req.HappRoutingProfile),
		HappColorProfile:           strings.TrimSpace(req.HappColorProfile),
		HappDirectSites:            strings.TrimSpace(req.HappDirectSites),
	})
	if err != nil {
		http.Error(w, "Failed to create subscription: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for position, member := range members {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: member.Username,
			Alias:    member.Alias,
			Position: int64(position),
		})
	}

	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllSubscriptions)
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

	now := s.now()
	usedBytes, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), store.GetSubscriptionUsageInRangeParams{
		SubID: id,
		Ts:    core.SubscriptionUsageWindowStart(sub.QuotaPeriod.String, now),
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
		Alias:                      sub.Alias,
		QuotaLimit:                 sub.QuotaLimit.Int64,
		QuotaPeriod:                sub.QuotaPeriod.String,
		UsedBytes:                  usedBytes,
		Users:                      subscriptionUsersFromMembers(members),
		Members:                    members,
		ProfileUpdateIntervalHours: nullableInt64Ptr(sub.ProfileUpdateIntervalHours),
		UpdateAlways:               int64ToBool(sub.UpdateAlways),
		HappRoutingProfile:         sub.HappRoutingProfile,
		HappColorProfile:           sub.HappColorProfile,
		HappDirectSites:            sub.HappDirectSites,
		LastRequestAt:              interfaceToInt64Ptr(sub.LastRequestAt),
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
		Alias:                      strings.TrimSpace(req.Alias),
		QuotaLimit:                 sql.NullInt64{Int64: req.QuotaLimit, Valid: true},
		QuotaPeriod:                sql.NullString{String: req.QuotaPeriod, Valid: true},
		ResetDay:                   sql.NullInt64{Int64: 1, Valid: true},
		ProfileUpdateIntervalHours: mergeNullableInt64(current.ProfileUpdateIntervalHours, req.ProfileUpdateIntervalHours),
		UpdateAlways:               boolPtrToInt64(req.UpdateAlways, int64ToBool(current.UpdateAlways)),
		HappRoutingProfile:         strings.TrimSpace(req.HappRoutingProfile),
		HappColorProfile:           strings.TrimSpace(req.HappColorProfile),
		HappDirectSites:            strings.TrimSpace(req.HappDirectSites),
		ID:                         id,
	})
	if err != nil {
		http.Error(w, "Failed to update subscription", http.StatusInternalServerError)
		return
	}

	s.store.Queries.ClearSubscriptionUsers(r.Context(), id)
	for position, member := range members {
		_ = s.store.Queries.AddUserToSubscription(r.Context(), store.AddUserToSubscriptionParams{
			SubID:    id,
			UserName: member.Username,
			Alias:    member.Alias,
			Position: int64(position),
		})
	}

	// Re-evaluate quota state after update (quota may have been raised, triggering re-enable)
	go s.store.EnforceSubscriptionQuotas(s.config)

	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllSubscriptions)
	detail := fmt.Sprintf("id:%d", id)
	if strings.TrimSpace(req.Name) != "" && req.Name != current.Name {
		detail += ",to:" + req.Name
	}
	s.insertAuditEntry(r, "subscription", "update", current.Name, detail)
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

// interfaceToInt64Ptr converts the interface{} that sqlc emits for nullable
// subquery columns (e.g. last_request_at) to a *int64. Returns nil when the
// value is nil, sql.NullInt64{Valid:false}, or any zero-value.
func interfaceToInt64Ptr(v interface{}) *int64 {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case int64:
		return &val
	case float64:
		i := int64(val)
		return &i
	case sql.NullInt64:
		if !val.Valid {
			return nil
		}
		return &val.Int64
	}
	return nil
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

	sub, err := s.store.Queries.GetSubscriptionByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	err = s.store.Queries.DeleteSubscription(r.Context(), id)
	if err != nil {
		http.Error(w, "Failed to delete subscription", http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllSubscriptions)
	s.insertAuditEntry(r, "subscription", "delete", sub.Name, fmt.Sprintf("id:%d", id))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRegenerateSubscriptionToken(w http.ResponseWriter, r *http.Request) {
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
	s.cache.Del(cacheKeyAllSubscriptions)
	s.insertAuditEntry(r, "subscription", "regenerate", sub.Name, fmt.Sprintf("id:%d", id))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (s *Server) handleEncryptHappLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		http.Error(w, "url must be http(s)", http.StatusBadRequest)
		return
	}

	outBody, err := json.Marshal(map[string]string{"url": req.URL})
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://crypto.happ.su/api-v2.php", bytes.NewReader(outBody))
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	outReq.Header.Set("Content-Type", "application/json")
	outReq.Header.Set("Accept", "application/json, text/plain")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "Happ crypto API failed"})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("happ crypto API: unexpected status %d, body=%q", resp.StatusCode, bodyBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "Happ crypto API failed"})
		return
	}

	// Attempt JSON decode first
	candidate := extractEncryptedHappLink(bodyBytes)

	if !strings.HasPrefix(candidate, "happ://crypt5/") {
		log.Printf("happ crypto API: unexpected response format, body=%q", bodyBytes)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "unexpected response from Happ crypto API"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"encrypted_url": candidate})
}

func extractEncryptedHappLink(bodyBytes []byte) string {
	var parsed struct {
		URL           string `json:"url"`
		EncryptedURL  string `json:"encrypted_url"`
		EncryptedLink string `json:"encrypted_link"`
		Result        string `json:"result"`
		Data          string `json:"data"`
	}
	if jsonErr := json.Unmarshal(bodyBytes, &parsed); jsonErr == nil {
		for _, v := range []string{parsed.EncryptedLink, parsed.EncryptedURL, parsed.URL, parsed.Result, parsed.Data} {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return strings.TrimSpace(string(bodyBytes))
}
