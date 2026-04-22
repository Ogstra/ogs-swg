package api

import "testing"

func TestClashLogsWebSocketURL(t *testing.T) {
	tests := []struct {
		name       string
		controller string
		want       string
	}{
		{
			name:       "host port",
			controller: "127.0.0.1:9090",
			want:       "ws://127.0.0.1:9090/logs",
		},
		{
			name:       "http URL",
			controller: "http://127.0.0.1:9090",
			want:       "ws://127.0.0.1:9090/logs",
		},
		{
			name:       "https URL with base path",
			controller: "https://example.invalid/clash/",
			want:       "wss://example.invalid/clash/logs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clashLogsWebSocketURL(tc.controller)
			if err != nil {
				t.Fatalf("clashLogsWebSocketURL() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("clashLogsWebSocketURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClashLogLine(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "clash payload",
			raw:  `{"type":"info","payload":"connection accepted"}`,
			want: "[INFO] connection accepted",
		},
		{
			name: "message fallback",
			raw:  `{"type":"warning","message":"controller warning"}`,
			want: "[WARNING] controller warning",
		},
		{
			name: "plain text fallback",
			raw:  `raw log line`,
			want: "raw log line",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clashLogLine(tc.raw); got != tc.want {
				t.Fatalf("clashLogLine() = %q, want %q", got, tc.want)
			}
		})
	}
}
