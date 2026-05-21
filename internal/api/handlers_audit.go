package api

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	page, err := s.auditStore.QueryAuditLog(r.Context(), limit, offset, domain, action)
	if err != nil {
		http.Error(w, "Failed to query audit log: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}
