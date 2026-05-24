package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	domain := r.URL.Query().Get("domain")
	action := r.URL.Query().Get("action")
	cacheKey := fmt.Sprintf("%s:v%d:limit=%d:offset=%d:domain=%s:action=%s", cacheKeyAuditLog, s.auditLogVer.Load(), limit, offset, domain, action)
	if cached, found := s.cache.Get(cacheKey); found {
		if b, ok := cached.([]byte); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
	}

	page, err := s.auditStore.QueryAuditLog(r.Context(), limit, offset, domain, action)
	if err != nil {
		http.Error(w, "Failed to query audit log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b, err := json.Marshal(page)
	if err != nil {
		http.Error(w, "Failed to encode audit log: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.cache.SetWithTTL(cacheKey, b, int64(len(b)), 15*time.Second)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}
