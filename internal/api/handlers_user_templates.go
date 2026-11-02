package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func validQuotaPeriod(period string) bool {
	switch period {
	case "daily", "weekly", "monthly", "yearly", "total":
		return true
	}
	return false
}

const (
	maxUserTemplateNameLength = 64
	maxUserTemplateExpireDays = 3650
)

func (s *Server) handleListUserTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.store.ListUserTemplates(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load templates")
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func decodeUserTemplate(r *http.Request) (core.UserTemplate, string) {
	var t core.UserTemplate
	if err := decodeJSON(r, &t); err != nil {
		return t, "Invalid request body"
	}
	t.Name = strings.TrimSpace(t.Name)
	t.InboundTag = strings.TrimSpace(t.InboundTag)
	t.Flow = strings.TrimSpace(t.Flow)
	t.QuotaPeriod = strings.TrimSpace(t.QuotaPeriod)
	if t.QuotaPeriod == "" {
		t.QuotaPeriod = "monthly"
	}
	switch {
	case t.Name == "":
		return t, "Name is required"
	case len(t.Name) > maxUserTemplateNameLength:
		return t, "Name is too long"
	case t.QuotaLimit < 0:
		return t, "quota_limit cannot be negative"
	case !validQuotaPeriod(t.QuotaPeriod):
		return t, "quota_period must be daily, weekly, monthly, yearly or total"
	case t.ExpireDays < 0 || t.ExpireDays > maxUserTemplateExpireDays:
		return t, "expire_days must be between 0 and 3650"
	}
	return t, ""
}

// checkTemplateRouteTags rejects ids that are not existing route tags.
func (s *Server) checkTemplateRouteTags(t *core.UserTemplate) string {
	if t.RouteTagIDs == nil {
		t.RouteTagIDs = []int64{}
	}
	for _, id := range t.RouteTagIDs {
		if _, err := s.store.GetUserRouteTag(id); err != nil {
			return "Unknown route tag " + strconv.FormatInt(id, 10)
		}
	}
	return ""
}

func (s *Server) handleCreateUserTemplate(w http.ResponseWriter, r *http.Request) {
	t, problem := decodeUserTemplate(r)
	if problem == "" {
		problem = s.checkTemplateRouteTags(&t)
	}
	if problem != "" {
		writeErr(w, http.StatusBadRequest, problem)
		return
	}
	id, err := s.store.CreateUserTemplate(r.Context(), t)
	if errors.Is(err, core.ErrUserTemplateExists) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to create template")
		return
	}
	t.ID = id
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleUpdateUserTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	t, problem := decodeUserTemplate(r)
	if problem == "" {
		problem = s.checkTemplateRouteTags(&t)
	}
	if problem != "" {
		writeErr(w, http.StatusBadRequest, problem)
		return
	}
	t.ID = id
	switch err := s.store.UpdateUserTemplate(r.Context(), t); {
	case errors.Is(err, core.ErrUserTemplateExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, core.ErrUserTemplateNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "Failed to update template")
	default:
		writeJSON(w, http.StatusOK, t)
	}
}

func (s *Server) handleDeleteUserTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	switch err := s.store.DeleteUserTemplate(r.Context(), id); {
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "Template not found")
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "Failed to delete template")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
