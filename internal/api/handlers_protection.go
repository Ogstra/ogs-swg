package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	store "github.com/Ogstra/ogs-swg/internal/core/store"
)

type protectionRuleCache struct {
	mu          sync.RWMutex
	ipBlocks    map[string]struct{}
	ipAllows    map[string]struct{}
	tokenBlocks map[string]struct{}
}

func newProtectionRuleCache() *protectionRuleCache {
	return &protectionRuleCache{
		ipBlocks:    make(map[string]struct{}),
		ipAllows:    make(map[string]struct{}),
		tokenBlocks: make(map[string]struct{}),
	}
}

func (c *protectionRuleCache) isIPBlocked(ip string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.ipBlocks[ip]
	return ok
}

func (c *protectionRuleCache) isIPAllowed(ip string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.ipAllows[ip]
	return ok
}

func (c *protectionRuleCache) isTokenBlocked(token string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.tokenBlocks[token]
	return ok
}

func (c *protectionRuleCache) reload(rules []store.ProtectionRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ipBlocks = make(map[string]struct{})
	c.ipAllows = make(map[string]struct{})
	c.tokenBlocks = make(map[string]struct{})

	for _, rule := range rules {
		switch rule.RuleType {
		case "ip_block":
			c.ipBlocks[rule.Value] = struct{}{}
		case "ip_allow":
			c.ipAllows[rule.Value] = struct{}{}
		case "token_block":
			c.tokenBlocks[rule.Value] = struct{}{}
		}
	}
}

func (s *Server) reloadProtectionRules(ctx context.Context) {
	if s == nil || s.store == nil || s.store.Queries == nil || s.protectionRules == nil {
		return
	}
	rules, err := s.store.Queries.GetAllProtectionRules(ctx)
	if err != nil {
		return
	}
	s.protectionRules.reload(rules)
}

func (s *Server) handleGetSubscriptionProtection(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.config.SubscriptionProtection)
}

func (s *Server) handleUpdateSubscriptionProtection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxRequests     int  `json:"max_requests"`
		WindowSeconds   int  `json:"window_seconds"`
		UAFilterEnabled bool `json:"ua_filter_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s.config.SubscriptionProtection.MaxRequests = req.MaxRequests
	s.config.SubscriptionProtection.WindowSeconds = req.WindowSeconds
	s.config.SubscriptionProtection.UAFilterEnabled = req.UAFilterEnabled
	s.config.SubscriptionProtection.MaxRequests = max(s.config.SubscriptionProtection.MaxRequests, 1)
	s.config.SubscriptionProtection.WindowSeconds = max(s.config.SubscriptionProtection.WindowSeconds, 1)

	if err := s.config.SaveAppConfig(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.config.SubscriptionProtection)
}

func (s *Server) handleGetProtectionRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.Queries.GetAllProtectionRules(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rules)
}

func (s *Server) handleCreateProtectionRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RuleType string `json:"rule_type"`
		Value    string `json:"value"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	req.RuleType = strings.TrimSpace(req.RuleType)
	req.Value = strings.TrimSpace(req.Value)
	req.Note = strings.TrimSpace(req.Note)
	switch req.RuleType {
	case "ip_block", "token_block", "ip_allow":
	default:
		http.Error(w, "invalid rule_type", http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		http.Error(w, "value is required", http.StatusBadRequest)
		return
	}

	if err := s.store.Queries.InsertProtectionRule(r.Context(), store.InsertProtectionRuleParams{
		RuleType:  req.RuleType,
		Value:     req.Value,
		Note:      req.Note,
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.reloadProtectionRules(r.Context())
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleDeleteProtectionRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.store.Queries.DeleteProtectionRule(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.reloadProtectionRules(r.Context())
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetBlockedLog(w http.ResponseWriter, r *http.Request) {
	limit := int64(50)
	offset := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	rows, err := s.store.Queries.GetBlockedSubscriptionRequests(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rows)
}
