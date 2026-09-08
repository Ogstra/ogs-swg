package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type capturedNtfyRequest struct {
	Method        string
	Path          string
	ContentType   string
	Authorization string
	Body          ntfyPublishBody
}

func newCapturingNtfyServer(t *testing.T, status int, respBody string) (*httptest.Server, *atomic.Int32, chan capturedNtfyRequest) {
	t.Helper()
	var count atomic.Int32
	captured := make(chan capturedNtfyRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var body ntfyPublishBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured <- capturedNtfyRequest{
			Method:        r.Method,
			Path:          r.URL.Path,
			ContentType:   r.Header.Get("Content-Type"),
			Authorization: r.Header.Get("Authorization"),
			Body:          body,
		}
		w.WriteHeader(status)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	return server, &count, captured
}

func TestPublishNtfyPayload(t *testing.T) {
	server, _, captured := newCapturingNtfyServer(t, http.StatusOK, "")
	defer server.Close()

	settings := NtfySettings{ServerURL: server.URL, Topic: "panel-alerts", AuthMode: "none"}

	cases := []struct {
		name     string
		msg      NtfyMessage
		wantTags []string
		wantPri  int
		wantTtl  string
	}{
		{"service_down", NtfyServiceDownMessage("sing-box"), []string{"rotating_light", "warning"}, 5, "Service Down: sing-box"},
		{"service_recovered", NtfyServiceRecoveredMessage("wireguard"), []string{"white_check_mark"}, 3, "Service Recovered: wireguard"},
		{"config_apply_failed", NtfyConfigApplyFailedMessage("bad json"), []string{"warning", "gear"}, 4, "Sing-box config apply failed"},
		{"crash_after_reload", NtfyCrashAfterReloadMessage(30 * time.Second), []string{"skull", "gear"}, 5, "Sing-box crashed after config reload"},
		{"high_traffic", NtfyHighTrafficMessage(1000, 500, time.Minute), []string{"chart_with_upwards_trend"}, 3, "High traffic"},
		{"test_message", NtfyTestMessage(), []string{"bell"}, 1, "Test notification"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := PublishNtfy(context.Background(), settings, tc.msg); err != nil {
				t.Fatalf("PublishNtfy: %v", err)
			}
			req := <-captured
			if req.Body.Title != tc.wantTtl {
				t.Errorf("title = %q, want %q", req.Body.Title, tc.wantTtl)
			}
			if !slices.Equal(req.Body.Tags, tc.wantTags) {
				t.Errorf("tags = %q, want %q", req.Body.Tags, tc.wantTags)
			}
			if req.Body.Priority != tc.wantPri {
				t.Errorf("priority = %d, want %d", req.Body.Priority, tc.wantPri)
			}
			if req.Body.Topic != "panel-alerts" {
				t.Errorf("topic = %q, want %q", req.Body.Topic, "panel-alerts")
			}
		})
	}
}

func TestPublishNtfyAuthModes(t *testing.T) {
	const token = "test-token-placeholder"
	const user = "testuser"
	const pass = "test-pass-placeholder"

	tests := []struct {
		name     string
		settings NtfySettings
		wantAuth string
	}{
		{
			name:     "none",
			settings: NtfySettings{AuthMode: "none"},
			wantAuth: "",
		},
		{
			name:     "bearer",
			settings: NtfySettings{AuthMode: "bearer", BearerToken: token},
			wantAuth: "Bearer " + token,
		},
		{
			name:     "basic",
			settings: NtfySettings{AuthMode: "basic", BasicUser: user, BasicPass: pass},
			wantAuth: "basic:" + user + ":" + pass, // decoded and compared below
		},
		{
			name:     "bearer_empty_token",
			settings: NtfySettings{AuthMode: "bearer", BearerToken: ""},
			wantAuth: "",
		},
		{
			name:     "basic_empty_creds",
			settings: NtfySettings{AuthMode: "basic", BasicUser: "", BasicPass: ""},
			wantAuth: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, captured := newCapturingNtfyServer(t, http.StatusOK, "")
			defer server.Close()

			settings := tc.settings
			settings.ServerURL = server.URL
			settings.Topic = "panel-alerts"

			if err := PublishNtfy(context.Background(), settings, NtfyTestMessage()); err != nil {
				t.Fatalf("PublishNtfy: %v", err)
			}
			req := <-captured

			if strings.HasPrefix(tc.wantAuth, "basic:") {
				parts := strings.SplitN(tc.wantAuth, ":", 3)
				wantUser, wantPass := parts[1], parts[2]
				if !strings.HasPrefix(req.Authorization, "Basic ") {
					t.Fatalf("Authorization = %q, want Basic prefix", req.Authorization)
				}
				decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(req.Authorization, "Basic "))
				if err != nil {
					t.Fatalf("decode basic auth: %v", err)
				}
				if string(decoded) != wantUser+":"+wantPass {
					t.Errorf("decoded basic auth = %q, want %q", decoded, wantUser+":"+wantPass)
				}
				return
			}

			if req.Authorization != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", req.Authorization, tc.wantAuth)
			}
		})
	}
}

func TestPublishNtfyErrorRedaction(t *testing.T) {
	const token = "test-token-placeholder"
	const pass = "test-pass-placeholder"

	settings := NtfySettings{
		Topic:       "panel-alerts",
		AuthMode:    "bearer",
		BearerToken: token,
	}

	t.Run("401_with_error_body", func(t *testing.T) {
		server, _, _ := newCapturingNtfyServer(t, http.StatusUnauthorized, `{"code":40101,"http":401,"error":"unauthorized"}`)
		defer server.Close()

		s := settings
		s.ServerURL = server.URL
		err := PublishNtfy(context.Background(), s, NtfyTestMessage())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unauthorized") || !strings.Contains(err.Error(), "401") {
			t.Errorf("error = %q, want it to mention unauthorized and 401", err.Error())
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), pass) {
			t.Errorf("error leaked credential: %q", err.Error())
		}
	})

	t.Run("500_empty_body", func(t *testing.T) {
		server, _, _ := newCapturingNtfyServer(t, http.StatusInternalServerError, "")
		defer server.Close()

		s := settings
		s.ServerURL = server.URL
		s.AuthMode = "basic"
		s.BasicUser = "testuser"
		s.BasicPass = pass
		err := PublishNtfy(context.Background(), s, NtfyTestMessage())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), pass) {
			t.Errorf("error leaked credential: %q", err.Error())
		}
	})

	t.Run("unreachable_url", func(t *testing.T) {
		s := settings
		s.ServerURL = "http://127.0.0.1:1"
		err := PublishNtfy(context.Background(), s, NtfyTestMessage())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), pass) {
			t.Errorf("error leaked credential: %q", err.Error())
		}
	})
}

func TestPublishNtfyNotConfigured(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cases := []NtfySettings{
		{ServerURL: "", Topic: "panel-alerts"},
		{ServerURL: server.URL, Topic: ""},
	}

	for i, s := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			err := PublishNtfy(context.Background(), s, NtfyTestMessage())
			if err == nil {
				t.Fatal("expected error for unconfigured settings, got nil")
			}
			if !strings.Contains(err.Error(), "not configured") {
				t.Errorf("error = %q, want it to mention 'not configured'", err.Error())
			}
		})
	}

	if count.Load() != 0 {
		t.Errorf("request counter = %d, want 0 (no HTTP request should be made)", count.Load())
	}
}

func TestNormalizeNtfySettings(t *testing.T) {
	t.Run("invalid_auth_mode_defaults_to_none", func(t *testing.T) {
		got := NormalizeNtfySettings(NtfySettings{AuthMode: "totally-invalid"})
		if got.AuthMode != "none" {
			t.Errorf("AuthMode = %q, want %q", got.AuthMode, "none")
		}
	})

	t.Run("switching_mode_clears_other_secrets", func(t *testing.T) {
		got := NormalizeNtfySettings(NtfySettings{
			AuthMode:    "bearer",
			BearerToken: "test-token-placeholder",
			BasicUser:   "testuser",
			BasicPass:   "test-pass-placeholder",
		})
		if got.BasicUser != "" || got.BasicPass != "" {
			t.Errorf("expected basic creds cleared for bearer mode, got user=%q pass=%q", got.BasicUser, got.BasicPass)
		}
		if got.BearerToken != "test-token-placeholder" {
			t.Errorf("expected bearer token preserved, got %q", got.BearerToken)
		}

		got2 := NormalizeNtfySettings(NtfySettings{
			AuthMode:    "basic",
			BearerToken: "test-token-placeholder",
			BasicUser:   "testuser",
			BasicPass:   "test-pass-placeholder",
		})
		if got2.BearerToken != "" {
			t.Errorf("expected bearer token cleared for basic mode, got %q", got2.BearerToken)
		}
	})

	t.Run("negative_threshold_clamps_to_zero", func(t *testing.T) {
		got := NormalizeNtfySettings(NtfySettings{TrafficThresholdBytes: -500})
		if got.TrafficThresholdBytes != 0 {
			t.Errorf("TrafficThresholdBytes = %d, want 0", got.TrafficThresholdBytes)
		}
	})
}

func TestNtfySettingsStringRedacts(t *testing.T) {
	const token = "test-token-placeholder"
	const pass = "test-pass-placeholder"
	settings := NtfySettings{
		ServerURL:   "https://ntfy.example.internal",
		Topic:       "panel-alerts",
		AuthMode:    "basic",
		BearerToken: token,
		BasicUser:   "testuser",
		BasicPass:   pass,
	}

	out := fmt.Sprintf("%v", settings)
	if strings.Contains(out, token) {
		t.Errorf("String() leaked bearer token: %q", out)
	}
	if strings.Contains(out, pass) {
		t.Errorf("String() leaked basic password: %q", out)
	}
}
