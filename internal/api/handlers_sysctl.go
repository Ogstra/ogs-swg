package api

import (
	"net/http"
	"strings"
)

// Sysctl Handlers

func (s *Server) handleGetSysctl(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeErr(w, http.StatusBadRequest, "Key is required")
		return
	}

	if s.executor != nil {
		val, err := s.executor.GetSysctl(r.Context(), key)
		if err != nil {
			// Check if error is due to whitelist
			if strings.Contains(strings.ToLower(err.Error()), "whitelist") {
				writeErr(w, http.StatusForbidden, err.Error())
				return
			}
			writeErr(w, http.StatusInternalServerError, "Failed to get sysctl: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"key":   key,
			"value": val,
		})
	} else {
		writeErr(w, http.StatusInternalServerError, "System executor not initialized")
	}
}

func (s *Server) handleApplySysctl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key" validate:"required"`
		Value string `json:"value" validate:"required"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.executor != nil {
		if err := s.executor.ApplySysctl(r.Context(), req.Key, req.Value); err != nil {
			// Check if error is due to whitelist
			if strings.Contains(strings.ToLower(err.Error()), "whitelist") {
				writeErr(w, http.StatusForbidden, err.Error())
				return
			}
			writeErr(w, http.StatusInternalServerError, "Failed to apply sysctl: "+err.Error())
			return
		}
	} else {
		writeErr(w, http.StatusInternalServerError, "System executor not initialized")
		return
	}

	w.WriteHeader(http.StatusOK)
}
