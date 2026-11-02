package api

import "net/http"

// handleGetSingboxPendingChanges answers only whether the saved Sing-box config
// still has changes the running service has not loaded. Pages that just need
// that flag poll this instead of the whole dashboard payload.
func (s *Server) handleGetSingboxPendingChanges(w http.ResponseWriter, _ *http.Request) {
	pending := false
	if s.config != nil {
		pending = s.config.GetSingboxPendingChanges()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"singbox_pending_changes": pending})
}
