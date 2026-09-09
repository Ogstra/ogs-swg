package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NtfySettings holds the operator-editable ntfy configuration, including
// optional auth credentials. Never log this struct or interpolate its
// credential fields into an error or log message — use String() (or the
// individual non-secret fields) for any diagnostic output.
type NtfySettings struct {
	ServerURL             string `json:"server_url"`
	Topic                 string `json:"topic"`
	AuthMode              string `json:"auth_mode"` // "none" | "bearer" | "basic"
	BearerToken           string `json:"bearer_token"`
	BasicUser             string `json:"basic_user"`
	BasicPass             string `json:"basic_pass"`
	EnableSingboxDown     bool   `json:"enable_singbox_down"`
	EnableWireguardDown   bool   `json:"enable_wireguard_down"`
	EnableHighTraffic     bool   `json:"enable_high_traffic"`
	EnableConfigErrors    bool   `json:"enable_config_errors"`
	TrafficThresholdBytes int64  `json:"traffic_threshold_bytes"`
}

// String implements fmt.Stringer so an accidental %v/%s format of a
// NtfySettings value cannot leak the bearer token or basic auth password.
func (s NtfySettings) String() string {
	setOrEmpty := func(v string) string {
		if v == "" {
			return "empty"
		}
		return "set"
	}
	return fmt.Sprintf("NtfySettings{server_url=%s, topic=%s, auth_mode=%s}",
		setOrEmpty(s.ServerURL), setOrEmpty(s.Topic), s.AuthMode)
}

// NtfyMessage is a single notification to publish via ntfy.
type NtfyMessage struct {
	Title    string
	Message  string
	Tags     []string
	Priority int
}

// NormalizeNtfySettings trims whitespace, coerces AuthMode to a known value,
// clears credentials that do not apply to the active auth mode, and clamps
// invalid numeric fields.
func NormalizeNtfySettings(s NtfySettings) NtfySettings {
	s.ServerURL = strings.TrimSpace(s.ServerURL)
	s.Topic = strings.TrimSpace(s.Topic)
	s.BasicUser = strings.TrimSpace(s.BasicUser)

	mode := strings.ToLower(strings.TrimSpace(s.AuthMode))
	switch mode {
	case "bearer", "basic":
		s.AuthMode = mode
	default:
		s.AuthMode = "none"
	}

	if s.AuthMode != "bearer" {
		s.BearerToken = ""
	}
	if s.AuthMode != "basic" {
		s.BasicUser = ""
		s.BasicPass = ""
	}

	if s.TrafficThresholdBytes < 0 {
		s.TrafficThresholdBytes = 0
	}

	return s
}

// Configured reports whether enough information is present to attempt a
// publish (a server URL and a topic).
func (s NtfySettings) Configured() bool {
	return s.ServerURL != "" && s.Topic != ""
}

type ntfyPublishBody struct {
	Topic    string `json:"topic"`
	Title    string `json:"title,omitempty"`
	Message  string `json:"message,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// SECURITY: PublishNtfy must never allow s.BearerToken, s.BasicPass, or a
// %+v/%v format of the NtfySettings struct into a returned error string or
// a log line. Only non-secret fields (server configured?, topic configured?,
// auth mode, HTTP status, response "error" field) may appear in error text.
func PublishNtfy(ctx context.Context, s NtfySettings, m NtfyMessage) error {
	s = NormalizeNtfySettings(s)
	if !s.Configured() {
		return errors.New("ntfy: not configured (server URL and topic are required)")
	}

	payload := ntfyPublishBody{
		Topic:    s.Topic,
		Title:    m.Title,
		Message:  m.Message,
		Tags:     m.Tags,
		Priority: m.Priority,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ntfy: encode request: %w", err)
	}

	url := strings.TrimRight(s.ServerURL, "/") + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ntfy: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	switch s.AuthMode {
	case "bearer":
		if s.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.BearerToken)
		}
	case "basic":
		if s.BasicUser != "" || s.BasicPass != "" {
			req.SetBasicAuth(s.BasicUser, s.BasicPass)
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		limited := io.LimitReader(resp.Body, 4096)
		_ = json.NewDecoder(limited).Decode(&errBody)
		if errBody.Error != "" {
			return fmt.Errorf("ntfy: %s (status %d)", errBody.Error, resp.StatusCode)
		}
		return fmt.Errorf("ntfy: unexpected status %d", resp.StatusCode)
	}

	return nil
}

// NtfyServiceDownMessage builds the notification for a monitored service
// going down.
func NtfyServiceDownMessage(service string) NtfyMessage {
	return NtfyMessage{
		Title:    fmt.Sprintf("Service Down: %s", service),
		Message:  fmt.Sprintf("%s is down.", service),
		Tags:     []string{"rotating_light", "warning"},
		Priority: 5,
	}
}

// NtfyServiceRecoveredMessage builds the notification for a monitored
// service recovering.
func NtfyServiceRecoveredMessage(service string) NtfyMessage {
	return NtfyMessage{
		Title:    fmt.Sprintf("Service Recovered: %s", service),
		Message:  fmt.Sprintf("%s has recovered.", service),
		Tags:     []string{"white_check_mark"},
		Priority: 3,
	}
}

// NtfyConfigApplyFailedMessage builds the notification for a failed
// sing-box config apply.
func NtfyConfigApplyFailedMessage(detail string) NtfyMessage {
	return NtfyMessage{
		Title:    "Sing-box config apply failed",
		Message:  detail,
		Tags:     []string{"warning", "gear"},
		Priority: 4,
	}
}

// NtfyCrashAfterReloadMessage builds the notification for a sing-box crash
// shortly after a config reload.
func NtfyCrashAfterReloadMessage(sinceReload time.Duration) NtfyMessage {
	return NtfyMessage{
		Title:    "Sing-box crashed after config reload",
		Message:  fmt.Sprintf("sing-box crashed %s after the last config reload.", sinceReload.Round(time.Second)),
		Tags:     []string{"skull", "gear"},
		Priority: 5,
	}
}

// NtfyHighTrafficMessage builds the notification for traffic exceeding the
// configured threshold within a window.
func NtfyHighTrafficMessage(totalBytes, thresholdBytes int64, window time.Duration) NtfyMessage {
	return NtfyMessage{
		Title:    "High traffic",
		Message:  fmt.Sprintf("Traffic reached %d bytes (threshold %d) over %s.", totalBytes, thresholdBytes, window.Round(time.Second)),
		Tags:     []string{"chart_with_upwards_trend"},
		Priority: 3,
	}
}

// NtfyTestMessage builds the notification used by the operator-triggered
// "send test notification" action.
func NtfyTestMessage() NtfyMessage {
	return NtfyMessage{
		Title:    "Test notification",
		Message:  "This is a test notification from the panel.",
		Tags:     []string{"bell"},
		Priority: 1,
	}
}
