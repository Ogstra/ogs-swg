package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
	"golang.org/x/net/websocket"
)

func (s *Server) handleClashLogsStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	clashAPI, err := s.getConfiguredClashAPI()
	if err != nil {
		http.Error(w, "failed to read Clash API config", http.StatusInternalServerError)
		return
	}
	if clashAPI == nil {
		http.Error(w, "Clash API logs unavailable", http.StatusServiceUnavailable)
		return
	}

	censored := false
	if p := getPermissions(r); p != nil && p.CanReadLogsCensored {
		censored = true
	}

	server := websocket.Server{
		Handler: func(client *websocket.Conn) {
			s.proxyClashLogs(r.Context(), client, clashAPI, censored)
		},
	}
	server.ServeHTTP(w, r)
}

func (s *Server) getConfiguredClashAPI() (*core.ClashAPI, error) {
	content, err := s.config.GetSingboxConfig()
	if err != nil {
		return nil, err
	}

	var cfg core.SingboxConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, err
	}
	if cfg.Experimental == nil || cfg.Experimental.ClashAPI == nil || cfg.Experimental.ClashAPI.ExternalController == "" {
		return nil, nil
	}
	return cfg.Experimental.ClashAPI, nil
}

func (s *Server) proxyClashLogs(ctx context.Context, client *websocket.Conn, api *core.ClashAPI, censored bool) {
	defer client.Close()

	upstreamURL, err := clashLogsWebSocketURL(api.ExternalController)
	if err != nil {
		log.Printf("clash logs websocket: invalid controller: %v", err)
		return
	}

	origin := "http://localhost"
	if client.Request() != nil && client.Request().Host != "" {
		scheme := "http"
		if client.Request().TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + client.Request().Host
	}

	cfg, err := websocket.NewConfig(upstreamURL, origin)
	if err != nil {
		log.Printf("clash logs websocket: config: %v", err)
		return
	}
	if api.Secret != "" {
		cfg.Header.Set("Authorization", "Bearer "+api.Secret)
	}

	upstream, err := cfg.DialContext(ctx)
	if err != nil {
		log.Printf("clash logs websocket: connect: %v", err)
		return
	}
	defer upstream.Close()

	for {
		var raw string
		if err := websocket.Message.Receive(upstream, &raw); err != nil {
			return
		}
		line := clashLogLine(raw)
		if line == "" {
			continue
		}
		lines := []string{line}
		sanitizeLogLines(lines)
		if censored {
			lines[0] = core.CensorLine(lines[0])
		}
		if err := websocket.Message.Send(client, lines[0]); err != nil {
			return
		}
	}
}

func clashLogsWebSocketURL(controller string) (string, error) {
	trimmed := strings.TrimSpace(controller)
	if trimmed == "" {
		return "", fmt.Errorf("empty controller")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/logs"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func clashLogLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var entry struct {
		Type    string `json:"type"`
		Payload string `json:"payload"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &entry); err != nil {
		return trimmed
	}

	text := strings.TrimSpace(entry.Payload)
	if text == "" {
		text = strings.TrimSpace(entry.Message)
	}
	if text == "" {
		return trimmed
	}

	if entry.Type == "" {
		return text
	}
	return "[" + strings.ToUpper(entry.Type) + "] " + text
}
