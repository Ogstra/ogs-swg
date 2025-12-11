package api

import (
	"encoding/json"
	"io"
	"net/http"
)

func (s *Server) handleGetSingboxConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	content, err := s.config.GetSingboxConfig()
	if err != nil {
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
}

func (s *Server) handleUpdateSingboxConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxConfig(string(body)); err != nil {
		http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSingboxInbounds(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	inbounds, err := s.config.GetSingboxInbounds()
	if err != nil {
		http.Error(w, "Failed to get inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbounds)
}

func (s *Server) handleAddSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	var newInbound map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&newInbound); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.AddSingboxInbound(newInbound); err != nil {
		http.Error(w, "Failed to add inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleUpdateSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "Missing tag query parameter", http.StatusBadRequest)
		return
	}

	var updatedInbound map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updatedInbound); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxInbound(tag, updatedInbound); err != nil {
		http.Error(w, "Failed to update inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "Missing tag query parameter", http.StatusBadRequest)
		return
	}

	if err := s.config.DeleteSingboxInbound(tag); err != nil {
		http.Error(w, "Failed to delete inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
