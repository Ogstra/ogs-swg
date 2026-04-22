package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type UserRouteTagStatus struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Color         string   `json:"color"`
	Description   string   `json:"description"`
	RuleMatchJSON string   `json:"rule_match_json"`
	Linked        bool     `json:"linked"`
	Broken        bool     `json:"broken"`
	BrokenReason  string   `json:"broken_reason,omitempty"`
	AuthUsers     []string `json:"auth_users"`
}

type CompatibleUserRouteRule struct {
	Index         int      `json:"index"`
	RuleMatchJSON string   `json:"rule_match_json"`
	Outbound      string   `json:"outbound"`
	AuthUsers     []string `json:"auth_users"`
	Summary       string   `json:"summary"`
	AlreadyLinked bool     `json:"already_linked"`
}

type userRouteTagWriteRequest struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	RuleIndex   *int   `json:"rule_index"`
}

type UpdateUserRouteTagsRequest struct {
	TagIDs []int64 `json:"tag_ids"`
}

type UpdateUserRouteTagsResponse struct {
	Success               bool                 `json:"success"`
	SingboxPendingChanges bool                 `json:"singbox_pending_changes"`
	RouteTags             []UserRouteTagStatus `json:"route_tags"`
}

func (s *Server) requireUserRouteTagDependencies(w http.ResponseWriter) bool {
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusInternalServerError)
		return false
	}
	if s.config == nil {
		http.Error(w, "sing-box config unavailable", http.StatusInternalServerError)
		return false
	}
	if !s.requireSingbox(w) {
		return false
	}
	return true
}

func routeTagStatusFromTag(tag core.UserRouteTag, resolution core.RouteTagRuleResolution) UserRouteTagStatus {
	status := UserRouteTagStatus{
		ID:            tag.ID,
		Name:          tag.Name,
		Color:         tag.Color,
		Description:   tag.Description,
		RuleMatchJSON: tag.RuleMatchJSON,
		AuthUsers:     []string{},
	}
	if resolution.Broken {
		status.Linked = false
		status.Broken = true
		status.BrokenReason = resolution.BrokenReason
		return status
	}
	status.Linked = true
	status.AuthUsers = resolution.AuthUsers
	if status.AuthUsers == nil {
		status.AuthUsers = []string{}
	}
	return status
}

func (s *Server) enrichUserRouteTag(tag core.UserRouteTag) (UserRouteTagStatus, error) {
	resolution, err := s.config.ResolveRouteTagRule(tag.RuleMatchJSON)
	if err != nil {
		return UserRouteTagStatus{}, err
	}
	return routeTagStatusFromTag(tag, resolution), nil
}

func (s *Server) routeTagStatusesForUser(userName string, tags []core.UserRouteTag) ([]UserRouteTagStatus, error) {
	assigned, err := s.config.ResolveUserRouteTags(userName, tags)
	if err != nil {
		return nil, err
	}
	statuses := make([]UserRouteTagStatus, 0, len(assigned))
	for _, tag := range assigned {
		status, err := s.enrichUserRouteTag(tag)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (s *Server) handleGetUserRouteTags(w http.ResponseWriter, _ *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	tags, err := s.store.ListUserRouteTags()
	if err != nil {
		http.Error(w, "Failed to list route tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	statuses := make([]UserRouteTagStatus, 0, len(tags))
	for _, tag := range tags {
		status, err := s.enrichUserRouteTag(tag)
		if err != nil {
			http.Error(w, "Failed to resolve route tag: "+err.Error(), http.StatusInternalServerError)
			return
		}
		statuses = append(statuses, status)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statuses)
}

func (s *Server) handleGetCompatibleUserRouteRules(w http.ResponseWriter, _ *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	tags, err := s.store.ListUserRouteTags()
	if err != nil {
		http.Error(w, "Failed to list route tags: "+err.Error(), http.StatusInternalServerError)
		return
	}
	linked := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if canonical, err := compactRouteTagMatch(tag.RuleMatchJSON); err == nil {
			linked[canonical] = struct{}{}
		}
	}

	rules, err := s.config.GetSingboxRouteRules()
	if err != nil {
		http.Error(w, "Failed to get route rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	compatible := []CompatibleUserRouteRule{}
	for i, rule := range rules {
		matchJSON, err := core.CanonicalRouteTagRuleMatch(rule)
		if err != nil {
			continue
		}
		authUsers, err := routeRuleAuthUsers(rule)
		if err != nil {
			continue
		}
		outbound, _ := rule["outbound"].(string)
		_, alreadyLinked := linked[matchJSON]
		compatible = append(compatible, CompatibleUserRouteRule{
			Index:         i,
			RuleMatchJSON: matchJSON,
			Outbound:      outbound,
			AuthUsers:     authUsers,
			Summary:       routeRuleSummary(rule),
			AlreadyLinked: alreadyLinked,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(compatible)
}

func (s *Server) handleCreateUserRouteTag(w http.ResponseWriter, r *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	var req userRouteTagWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.RuleIndex == nil {
		http.Error(w, "rule_index is required", http.StatusBadRequest)
		return
	}

	ruleMatchJSON, err := s.ruleMatchJSONForIndex(*req.RuleIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tag, err := s.store.CreateUserRouteTag(req.Name, req.Color, req.Description, ruleMatchJSON)
	if err != nil {
		http.Error(w, "Failed to create route tag: "+err.Error(), statusForRouteTagStoreError(err))
		return
	}
	status, err := s.enrichUserRouteTag(tag)
	if err != nil {
		http.Error(w, "Failed to resolve route tag: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleUpdateUserRouteTag(w http.ResponseWriter, r *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	id, err := parseRouteTagID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := s.store.GetUserRouteTag(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Route tag not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get route tag: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var req userRouteTagWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	ruleMatchJSON := existing.RuleMatchJSON
	if req.RuleIndex != nil {
		ruleMatchJSON, err = s.ruleMatchJSONForIndex(*req.RuleIndex)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	updated := core.UserRouteTag{
		ID:            existing.ID,
		Name:          req.Name,
		Color:         req.Color,
		Description:   req.Description,
		RuleMatchJSON: ruleMatchJSON,
	}
	if err := s.store.UpdateUserRouteTag(updated); err != nil {
		http.Error(w, "Failed to update route tag: "+err.Error(), statusForRouteTagStoreError(err))
		return
	}
	tag, err := s.store.GetUserRouteTag(id)
	if err != nil {
		http.Error(w, "Failed to get route tag: "+err.Error(), http.StatusInternalServerError)
		return
	}
	status, err := s.enrichUserRouteTag(*tag)
	if err != nil {
		http.Error(w, "Failed to resolve route tag: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleDeleteUserRouteTag(w http.ResponseWriter, r *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	id, err := parseRouteTagID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteUserRouteTag(id); err != nil {
		http.Error(w, "Failed to delete route tag: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateUserRouteTags(w http.ResponseWriter, r *http.Request) {
	if !s.requireUserRouteTagDependencies(w) {
		return
	}

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	var req UpdateUserRouteTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tags, err := s.store.ListUserRouteTags()
	if err != nil {
		http.Error(w, "Failed to list route tags: "+err.Error(), http.StatusInternalServerError)
		return
	}
	assigned, err := s.config.UpdateUserRouteTagMembership(name, req.TagIDs, tags)
	if err != nil {
		if strings.Contains(err.Error(), "route tag needs relink") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "Failed to update route tags: "+err.Error(), http.StatusInternalServerError)
		return
	}

	statuses := make([]UserRouteTagStatus, 0, len(assigned))
	for _, tag := range assigned {
		status, err := s.enrichUserRouteTag(tag)
		if err != nil {
			http.Error(w, "Failed to resolve route tag: "+err.Error(), http.StatusInternalServerError)
			return
		}
		statuses = append(statuses, status)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(UpdateUserRouteTagsResponse{
		Success:               true,
		SingboxPendingChanges: s.config.GetSingboxPendingChanges(),
		RouteTags:             statuses,
	})
}

func parseRouteTagID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid route tag id")
	}
	return id, nil
}

func (s *Server) ruleMatchJSONForIndex(index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("invalid rule_index")
	}
	rules, err := s.config.GetSingboxRouteRules()
	if err != nil {
		return "", fmt.Errorf("failed to get route rules: %w", err)
	}
	if index >= len(rules) {
		return "", fmt.Errorf("rule_index out of range")
	}
	ruleMatchJSON, err := core.CanonicalRouteTagRuleMatch(rules[index])
	if err != nil {
		return "", err
	}
	return ruleMatchJSON, nil
}

func routeRuleAuthUsers(rule map[string]interface{}) ([]string, error) {
	raw, ok := rule["auth_user"]
	if !ok {
		return nil, fmt.Errorf("auth_user is required")
	}
	switch value := raw.(type) {
	case string:
		user := strings.TrimSpace(value)
		if user == "" {
			return []string{}, nil
		}
		return []string{user}, nil
	case []interface{}:
		users := make([]string, 0, len(value))
		seen := make(map[string]struct{}, len(value))
		for _, rawUser := range value {
			user, ok := rawUser.(string)
			if !ok {
				return nil, fmt.Errorf("auth_user must contain strings")
			}
			user = strings.TrimSpace(user)
			if user == "" {
				continue
			}
			if _, exists := seen[user]; exists {
				continue
			}
			seen[user] = struct{}{}
			users = append(users, user)
		}
		return users, nil
	case []string:
		users := make([]string, 0, len(value))
		seen := make(map[string]struct{}, len(value))
		for _, rawUser := range value {
			user := strings.TrimSpace(rawUser)
			if user == "" {
				continue
			}
			if _, exists := seen[user]; exists {
				continue
			}
			seen[user] = struct{}{}
			users = append(users, user)
		}
		return users, nil
	default:
		return nil, fmt.Errorf("auth_user must be a string or string array")
	}
}

func routeRuleSummary(rule map[string]interface{}) string {
	outbound, _ := rule["outbound"].(string)
	parts := []string{}
	if action, _ := rule["action"].(string); strings.TrimSpace(action) != "" {
		parts = append(parts, "action="+strings.TrimSpace(action))
	}
	if strings.TrimSpace(outbound) != "" {
		parts = append(parts, "outbound="+strings.TrimSpace(outbound))
	}
	if inbound, ok := rule["inbound"]; ok {
		if data, err := json.Marshal(inbound); err == nil {
			parts = append(parts, "inbound="+string(data))
		}
	}
	if ruleSet, ok := rule["rule_set"]; ok {
		if data, err := json.Marshal(ruleSet); err == nil {
			parts = append(parts, "rule_set="+string(data))
		}
	}
	if len(parts) == 0 {
		return "route rule"
	}
	return strings.Join(parts, " ")
}

func compactRouteTagMatch(input string) (string, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(strings.TrimSpace(input))); err != nil {
		return "", err
	}
	return out.String(), nil
}

func statusForRouteTagStoreError(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return http.StatusConflict
	}
	if strings.Contains(strings.ToLower(err.Error()), "required") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
