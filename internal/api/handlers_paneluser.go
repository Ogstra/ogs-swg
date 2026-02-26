package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func ensureGrantablePermissions(caller *core.PanelUserPermissions, requested core.PanelUserPermissions) error {
	if caller == nil {
		return nil
	}
	c := *caller
	c.Normalize()
	requested.Normalize()

	checks := []struct {
		name    string
		hasPerm bool
		grants  bool
	}{
		{"can_read_users", c.CanReadUsers, requested.CanReadUsers},
		{"can_write_users", c.CanWriteUsers, requested.CanWriteUsers},
		{"can_read_wireguard", c.CanReadWireguard, requested.CanReadWireguard},
		{"can_write_wireguard", c.CanWriteWireguard, requested.CanWriteWireguard},
		{"can_read_config", c.CanReadConfig, requested.CanReadConfig},
		{"can_write_config", c.CanWriteConfig, requested.CanWriteConfig},
		{"can_read_settings", c.CanReadSettings, requested.CanReadSettings},
		{"can_write_settings", c.CanWriteSettings, requested.CanWriteSettings},
		{"can_read_panel_users", c.CanReadPanelUsers, requested.CanReadPanelUsers},
		{"can_write_panel_users", c.CanWritePanelUsers, requested.CanWritePanelUsers},
		{"can_read_logs", c.CanReadLogs, requested.CanReadLogs},
	}

	for _, chk := range checks {
		if chk.grants && !chk.hasPerm {
			return fmt.Errorf("cannot grant %s: caller does not have this permission", chk.name)
		}
	}
	return nil
}

func (s *Server) handleGetPanelUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.GetAllPanelUsers()
	if err != nil {
		http.Error(w, "Failed to fetch panel users: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

type createPanelUserRequest struct {
	Username    string                    `json:"username" validate:"required"`
	Password    string                    `json:"password" validate:"required,min=8"`
	Permissions core.PanelUserPermissions `json:"permissions"`
}

func (s *Server) handleCreatePanelUser(w http.ResponseWriter, r *http.Request) {
	var req createPanelUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Permissions.Normalize()

	// Prevent privilege escalation: caller cannot grant permissions they don't have.
	if err := ensureGrantablePermissions(getPermissions(r), req.Permissions); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := s.store.CreatePanelUser(req.Username, req.Password, req.Permissions); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, "Username already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create panel user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

type updatePanelUserPermissionsRequest struct {
	Username    string                    `json:"username" validate:"required"`
	Permissions core.PanelUserPermissions `json:"permissions"`
}

func (s *Server) handleUpdatePanelUserPermissions(w http.ResponseWriter, r *http.Request) {
	var req updatePanelUserPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.Permissions.Normalize()

	// Prevent privilege escalation.
	if err := ensureGrantablePermissions(getPermissions(r), req.Permissions); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := s.store.UpdatePanelUserPermissions(req.Username, req.Permissions); err != nil {
		http.Error(w, "Failed to update permissions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type updatePanelUserUsernameRequest struct {
	Username    string `json:"username" validate:"required"`
	NewUsername string `json:"new_username" validate:"required"`
}

func (s *Server) handleUpdatePanelUserUsername(w http.ResponseWriter, r *http.Request) {
	var req updatePanelUserUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.UpdatePanelUsername(req.Username, req.NewUsername); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, "Failed to update username: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type updatePanelUserPasswordRequest struct {
	Username    string `json:"username" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

func (s *Server) handleUpdatePanelUserPassword(w http.ResponseWriter, r *http.Request) {
	var req updatePanelUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.store.UpdatePanelUserPassword(req.Username, req.NewPassword); err != nil {
		http.Error(w, "Failed to update password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeletePanelUser(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		http.Error(w, "username query parameter is required", http.StatusBadRequest)
		return
	}

	// Prevent self-deletion
	claims, _ := r.Context().Value(userContextKey).(map[string]interface{})
	if sub, _ := claims["sub"].(string); sub == username {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	if err := s.store.DeletePanelUser(username); err != nil {
		http.Error(w, "Failed to delete panel user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
