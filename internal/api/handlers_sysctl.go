package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Sysctl Handlers

func (s *Server) handleGetSysctl(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	if s.executor != nil {
		val, err := s.executor.GetSysctl(r.Context(), key)
		if err != nil {
			// Check if error is due to whitelist
			if strings.Contains(strings.ToLower(err.Error()), "whitelist") {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			http.Error(w, "Failed to get sysctl: "+err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"key":   key,
			"value": val,
		})
	} else {
		http.Error(w, "System executor not initialized", http.StatusInternalServerError)
	}
}

func (s *Server) handleApplySysctl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key" validate:"required"`
		Value string `json:"value" validate:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.executor != nil {
		if err := s.executor.ApplySysctl(r.Context(), req.Key, req.Value); err != nil {
			// Check if error is due to whitelist
			if strings.Contains(strings.ToLower(err.Error()), "whitelist") {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			http.Error(w, "Failed to apply sysctl: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "System executor not initialized", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
