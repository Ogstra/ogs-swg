package api

import (
	"context"
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

// AuditLogger wraps handler h. On 2xx response, writes one audit_log entry.
func (s *Server) AuditLogger(domain, action string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sw := &statusCapturingWriter{ResponseWriter: w}
		h(sw, r)
		if code := sw.status200(); code >= 200 && code < 300 {
			if s.auditStore == nil {
				return
			}
			entry := core.AuditEntry{
				Ts:     time.Now().Unix(),
				Actor:  requestActor(r),
				IP:     requestAuditIP(r),
				Action: action,
				Domain: domain,
			}
			if err := s.auditStore.InsertAuditLog(context.Background(), entry); err != nil {
				log.Printf("audit: insert failed: %v", err)
			}
		}
	}
}
