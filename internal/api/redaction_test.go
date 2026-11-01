package api

import "testing"

func TestRedactJSONValue_MasksACMEAPIToken(t *testing.T) {
	in := map[string]any{
		"tls": map[string]any{
			"server_name": "vpn.example.test",
			"acme": map[string]any{
				"domain":          []any{"vpn.example.test"},
				"dns01_challenge": map[string]any{"provider": "cloudflare", "api_token": "token-placeholder"},
			},
		},
	}
	out := redactJSONValue(in).(map[string]any)
	acme := out["tls"].(map[string]any)["acme"].(map[string]any)
	challenge := acme["dns01_challenge"].(map[string]any)
	if challenge["api_token"] == "token-placeholder" {
		t.Fatal("api_token was not masked")
	}
	if challenge["provider"] != "cloudflare" {
		t.Fatalf("provider = %v; only secrets should be masked", challenge["provider"])
	}
}
