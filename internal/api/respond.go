package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes v as a JSON response with the given status code.
// It replaces the inline `w.Header().Set("Content-Type","application/json"); json.NewEncoder(w).Encode(v)`
// pattern. Pass http.StatusOK for the common implicit-200 case.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes a plain-text error response identical to http.Error.
// It exists so handlers route errors through one call site; it intentionally
// preserves http.Error semantics (text/plain; charset=utf-8, nosniff, msg+"\n")
// because existing tests assert on the plain-text body.
func writeErr(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// decodeJSON decodes the request body into v and returns any decode error.
// It does NOT write a response — callers keep their existing error message and status.
func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
