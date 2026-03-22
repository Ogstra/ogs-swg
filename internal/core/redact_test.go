package core

import "testing"

func TestCensorLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "accepted IPv4 connection",
			input: "INFO accepted from 1.2.3.4:5678 to example.com:443",
			want:  "INFO accepted from ***:*** to ***:***",
		},
		{
			name:  "rejected IPv4 connection",
			input: "INFO rejected from 192.168.1.100:9999 to 10.0.0.1:80",
			want:  "INFO rejected from ***:*** to ***:***",
		},
		{
			name:  "non-connection log line",
			input: "sing-box started",
			want:  "sing-box started",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "IPv6 bracket notation",
			input: "INFO accepted from [::1]:8080 to example.com:443",
			want:  "INFO accepted from ***:*** to ***:***",
		},
		{
			name:  "only from — no to present",
			input: "INFO accepted from 1.2.3.4:1234 via some-route",
			want:  "INFO accepted from ***:*** via some-route",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CensorLine(tc.input)
			if got != tc.want {
				t.Errorf("CensorLine(%q)\n got  %q\n want %q", tc.input, got, tc.want)
			}
		})
	}
}
