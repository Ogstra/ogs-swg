package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// statusCapturingWriter captures the HTTP status code written by the handler.
type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingWriter) status200() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// requestActor extracts the authenticated panel username, or "system" for API-key auth.
func requestActor(r *http.Request) string {
	if username, ok := currentPanelUsername(r); ok {
		return username
	}
	return "system"
}

// requestAuditIP extracts the real client IP for audit purposes.
func requestAuditIP(r *http.Request) string {
	if isTrustedProxy(r.RemoteAddr) {
		if ip := firstHeaderToken(r.Header.Get("X-Real-IP")); ip != "" {
			return stripPort(ip)
		}
		if ip := firstHeaderToken(r.Header.Get("X-Forwarded-For")); ip != "" {
			return stripPort(ip)
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// auditEntityAndDetail extracts entity_id and detail from the request.
// Priority: path values → query params → body JSON fields.
// Body is only read for POST/PUT and only up to 8 KB.
func auditEntityAndDetail(r *http.Request, body []byte, domain, action string) (entityID, detail string) {
	// 1. Path values (Go 1.22 ServeMux pattern params)
	for _, key := range []string{"name", "id", "iface", "tag"} {
		if v := r.PathValue(key); v != "" {
			entityID = v
			break
		}
	}

	// 2. Query params (for DELETE endpoints that use ?name= or ?username= or ?tag=)
	if entityID == "" {
		for _, key := range []string{"name", "username", "tag", "id"} {
			if v := r.URL.Query().Get(key); v != "" {
				entityID = v
				break
			}
		}
	}

	// 3. Body JSON fields (safe fields only — never password/secret fields)
	if entityID == "" && len(body) > 0 {
		var m map[string]interface{}
		if json.Unmarshal(body, &m) == nil {
			for _, key := range []string{"name", "username", "new_username", "service", "tag", "public_key", "iface_name", "title"} {
				if v, ok := m[key].(string); ok && v != "" {
					entityID = v
					break
				}
			}
		}
		if entityID == "" {
			var items []map[string]interface{}
			if json.Unmarshal(body, &items) == nil && len(items) > 0 {
				names := auditNamesFromItems(items)
				if len(names) > 0 {
					entityID = fmt.Sprintf("users:%d", len(names))
				}
			}
		}
	}

	// 4. Fallback: use actor for auth/self-service updates without path param
	if entityID == "" && (domain == "auth" || domain == "panel_user") {
		if username, ok := currentPanelUsername(r); ok {
			entityID = username
		}
	}

	// detail: bulk ops, or name+id combo when both available
	detail = auditDetail(r, body, entityID)
	return
}

func auditDetail(r *http.Request, body []byte, entityID string) string {
	if len(body) > 0 {
		var m map[string]interface{}
		if json.Unmarshal(body, &m) == nil {
			if oldUsername, _ := m["username"].(string); oldUsername != "" {
				if newUsername, _ := m["new_username"].(string); newUsername != "" && newUsername != oldUsername {
					return "to:" + newUsername
				}
			}
			if ids, ok := m["ids"].([]interface{}); ok {
				return fmt.Sprintf("ids:%d", len(ids))
			}
		}
		var items []map[string]interface{}
		if json.Unmarshal(body, &items) == nil && len(items) > 0 {
			names := auditNamesFromItems(items)
			if len(names) > 0 {
				const maxNames = 12
				shown := names
				if len(shown) > maxNames {
					shown = shown[:maxNames]
				}
				detail := fmt.Sprintf("users:%d:%s", len(names), strings.Join(shown, ","))
				if len(names) > maxNames {
					detail += ",..."
				}
				return detail
			}
		}
	}
	// Bulk via query param (e.g. ?sub_id=N clears all for a sub)
	if subID := r.URL.Query().Get("sub_id"); subID != "" {
		return fmt.Sprintf("sub:%s", subID)
	}
	return ""
}

func auditNamesFromItems(items []map[string]interface{}) []string {
	names := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		for _, key := range []string{"name", "username"} {
			if v, ok := item[key].(string); ok {
				name := strings.TrimSpace(v)
				if name == "" {
					continue
				}
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
				break
			}
		}
	}
	return names
}

func shortAuditID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (s *Server) insertAuditEntry(r *http.Request, domain, action, entityID, detail string) {
	if s.auditStore == nil {
		return
	}
	entry := core.AuditEntry{
		Ts:       time.Now().Unix(),
		Actor:    requestActor(r),
		IP:       requestAuditIP(r),
		Action:   action,
		Domain:   domain,
		EntityID: entityID,
		Detail:   detail,
	}
	if err := s.auditStore.InsertAuditLog(context.Background(), entry); err != nil {
		log.Printf("audit: insert failed: %v", err)
		return
	}
	s.invalidateAuditLogCache()
}

// AuditLogger wraps handler h. On 2xx response, writes one audit_log entry
// with entity_id and detail extracted from path values, query params, and body.
func (s *Server) AuditLogger(domain, action string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Buffer body for POST/PUT so we can extract entity info AND still pass it to the handler.
		var bodyBytes []byte
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			bodyBytes, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			if len(bodyBytes) > 8192 {
				bodyBytes = bodyBytes[:8192]
			}
		}

		sw := &statusCapturingWriter{ResponseWriter: w}
		h(sw, r)

		if code := sw.status200(); code >= 200 && code < 300 {
			if s.auditStore == nil {
				return
			}
			entityID, detail := auditEntityAndDetail(r, bodyBytes, domain, action)
			s.insertAuditEntry(r, domain, action, entityID, detail)
		}
	}
}
