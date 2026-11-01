package api

import "net/http"

type outboundFromLinkRequest struct {
	Link string `json:"link"`
}

// handleOutboundFromLink converts a pasted share link into an outbound object.
// It only converts: the caller decides the tag and writes the config.
func (s *Server) handleOutboundFromLink(w http.ResponseWriter, r *http.Request) {
	var req outboundFromLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	outbound, err := outboundFromShareLink(req.Link)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, outbound)
}
