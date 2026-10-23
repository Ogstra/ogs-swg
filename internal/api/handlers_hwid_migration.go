package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type subscriptionDeviceResponse struct {
	core.SubscriptionDevice
	Migration *core.HWIDMigration `json:"migration"`
}

func (s *Server) handleListSubscriptionDevices(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	if _, err := s.store.Queries.GetSubscriptionByID(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "Subscription not found")
		return
	}
	devices, err := s.store.ListSubscriptionDevices(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load devices")
		return
	}
	migrations, err := s.store.ListHWIDMigrations(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load migrations")
		return
	}
	// ListHWIDMigrations is newest first, so the first row per hash wins.
	latest := make(map[string]*core.HWIDMigration, len(migrations))
	for i := range migrations {
		if _, ok := latest[migrations[i].HWIDHash]; !ok {
			latest[migrations[i].HWIDHash] = &migrations[i]
		}
	}
	out := make([]subscriptionDeviceResponse, 0, len(devices))
	for _, d := range devices {
		// Rows are stored as the client sent them; names are resolved on read like the request history does.
		d.DeviceModel = resolveSubscriptionDeviceModel(d.DeviceModel)
		out = append(out, subscriptionDeviceResponse{SubscriptionDevice: d, Migration: latest[d.HWIDHash]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateHWIDMigration(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	var req struct {
		HWIDHash    string `json:"hwid_hash"`
		TargetSubID int64  `json:"target_sub_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.HWIDHash = strings.TrimSpace(req.HWIDHash)
	if req.HWIDHash == "" {
		writeErr(w, http.StatusBadRequest, "hwid_hash is required")
		return
	}
	if req.TargetSubID <= 0 {
		writeErr(w, http.StatusBadRequest, "target_sub_id is required")
		return
	}
	if req.TargetSubID == id {
		writeErr(w, http.StatusBadRequest, "Target subscription must differ from the source")
		return
	}
	if _, err := s.store.Queries.GetSubscriptionByID(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "Subscription not found")
		return
	}
	if _, err := s.store.Queries.GetSubscriptionByID(r.Context(), req.TargetSubID); err != nil {
		writeErr(w, http.StatusNotFound, "Target subscription not found")
		return
	}
	known, err := s.store.HasSubscriptionKnownHWID(r.Context(), id, req.HWIDHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to check device")
		return
	}
	if !known {
		writeErr(w, http.StatusConflict, "Device is not known for this subscription")
		return
	}
	// A confirmed move is final; it is not offered again.
	existing, err := s.store.ListHWIDMigrations(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load migrations")
		return
	}
	for _, m := range existing {
		if m.HWIDHash == req.HWIDHash {
			if m.Status == "confirmed" {
				writeErr(w, http.StatusConflict, "Device was already moved")
				return
			}
			break
		}
	}
	if err := s.store.CreateHWIDMigration(r.Context(), id, req.HWIDHash, req.TargetSubID, s.now().Unix()); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to create migration")
		return
	}
	m, err := s.store.GetLiveHWIDMigration(r.Context(), id, req.HWIDHash)
	if err != nil || m == nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load migration")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleCancelHWIDMigration(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	hash := strings.TrimSpace(r.PathValue("hwid_hash"))
	if hash == "" {
		writeErr(w, http.StatusBadRequest, "hwid_hash is required")
		return
	}
	if _, err := s.store.Queries.GetSubscriptionByID(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "Subscription not found")
		return
	}
	if err := s.store.CancelHWIDMigration(r.Context(), id, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to cancel migration")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteSubscriptionDevice removes a device that has already been moved,
// so the operator can clear it from the list. Other devices are not removable.
func (s *Server) handleDeleteSubscriptionDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid ID")
		return
	}
	hash := strings.TrimSpace(r.PathValue("hwid_hash"))
	if hash == "" {
		writeErr(w, http.StatusBadRequest, "hwid_hash is required")
		return
	}
	if _, err := s.store.Queries.GetSubscriptionByID(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "Subscription not found")
		return
	}
	migrations, err := s.store.ListHWIDMigrations(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load migrations")
		return
	}
	moved := false
	for _, m := range migrations {
		if m.HWIDHash == hash {
			moved = m.Status == "confirmed"
			break
		}
	}
	if !moved {
		writeErr(w, http.StatusConflict, "Only devices that have been moved can be removed")
		return
	}
	if err := s.store.DeleteSubscriptionDevice(r.Context(), id, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to remove device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCancelAllHWIDMigrations is the operator kill switch: it stops every
// live migration without a redeploy.
func (s *Server) handleCancelAllHWIDMigrations(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CancelAllHWIDMigrations(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to cancel migrations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"cancelled": n})
}
