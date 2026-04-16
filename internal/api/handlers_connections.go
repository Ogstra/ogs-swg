package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type ConnectionsResponse struct {
	Realtime bool             `json:"realtime"`
	Users    []ConnectionUser `json:"users"`
}

type ConnectionUser struct {
	Name        string `json:"name"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	Connections int    `json:"connections"`
}

type clashConnectionsResponse struct {
	Connections []clashConnection `json:"connections"`
}

type clashConnection struct {
	Metadata clashConnectionMetadata `json:"metadata"`
	Upload   int64                   `json:"upload"`
	Download int64                   `json:"download"`
}

type clashConnectionMetadata struct {
	Type string `json:"type"`
}

func (s *Server) handleGetConnections(w http.ResponseWriter, r *http.Request) {
	resp := ConnectionsResponse{
		Realtime: false,
		Users:    s.getApproximateConnections(),
	}

	api, err := s.config.GetClashAPISettings()
	if err != nil {
		log.Printf("connections: load clash api settings: %v", err)
	} else if api != nil {
		users, err := fetchClashConnections(api)
		if err != nil {
			log.Printf("connections: clash api fallback: %v", err)
		} else {
			resp.Realtime = true
			resp.Users = users
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) getApproximateConnections() []ConnectionUser {
	var names []string

	if users, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
		names = users
	}
	if len(names) == 0 {
		if users, err := s.store.GetActiveUsers(5 * time.Minute); err == nil {
			names = users
		}
	}
	if len(names) == 0 && s.config.DemoMode {
		names = s.demoActiveSingboxUsers(time.Now())
	}

	return connectionUsersFromNames(names)
}

func connectionUsersFromNames(names []string) []ConnectionUser {
	if len(names) == 0 {
		return []ConnectionUser{}
	}

	seen := make(map[string]struct{}, len(names))
	users := make([]ConnectionUser, 0, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		users = append(users, ConnectionUser{Name: name})
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})

	return users
}

func fetchClashConnections(api *core.ClashAPI) ([]ConnectionUser, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/connections", api.ExternalController), nil)
	if err != nil {
		return nil, fmt.Errorf("create clash api request: %w", err)
	}
	if api.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+api.Secret)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clash api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clash api unexpected status %d", resp.StatusCode)
	}

	var payload clashConnectionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode clash api response: %w", err)
	}

	return aggregateClashConnections(payload.Connections), nil
}

func aggregateClashConnections(connections []clashConnection) []ConnectionUser {
	if len(connections) == 0 {
		return []ConnectionUser{}
	}

	byUser := make(map[string]*ConnectionUser, len(connections))
	for _, conn := range connections {
		name := connectionUserName(conn.Metadata.Type)
		entry, ok := byUser[name]
		if !ok {
			entry = &ConnectionUser{Name: name}
			byUser[name] = entry
		}
		entry.Upload += conn.Upload
		entry.Download += conn.Download
		entry.Connections++
	}

	users := make([]ConnectionUser, 0, len(byUser))
	for _, user := range byUser {
		users = append(users, *user)
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].Connections != users[j].Connections {
			return users[i].Connections > users[j].Connections
		}
		return users[i].Name < users[j].Name
	})

	return users
}

func connectionUserName(rawType string) string {
	trimmed := strings.TrimSpace(rawType)
	if trimmed == "" {
		return "unknown"
	}

	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 2 {
		name := strings.TrimSpace(parts[1])
		if name != "" {
			return name
		}
	}

	return trimmed
}
