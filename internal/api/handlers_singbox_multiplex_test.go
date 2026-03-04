package api

import "testing"

func TestNormalizeInboundMultiplex_DisabledRemovesField(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": false,
			"padding": true,
		},
	}

	if err := normalizeInboundMultiplex(inbound); err != nil {
		t.Fatalf("normalizeInboundMultiplex() error = %v", err)
	}

	if _, ok := inbound["multiplex"]; ok {
		t.Fatalf("expected multiplex to be removed when disabled")
	}
}

func TestNormalizeInboundMultiplex_EnabledWithBrutal(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": true,
			"padding": true,
			"brutal": map[string]interface{}{
				"enabled":   true,
				"up_mbps":   "100",
				"down_mbps": float64(200),
			},
		},
	}

	if err := normalizeInboundMultiplex(inbound); err != nil {
		t.Fatalf("normalizeInboundMultiplex() error = %v", err)
	}

	multiplex, ok := inbound["multiplex"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected multiplex map, got %#v", inbound["multiplex"])
	}
	if enabled, _ := multiplex["enabled"].(bool); !enabled {
		t.Fatalf("expected multiplex.enabled=true")
	}
	if padding, _ := multiplex["padding"].(bool); !padding {
		t.Fatalf("expected multiplex.padding=true")
	}

	brutal, ok := multiplex["brutal"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected brutal map, got %#v", multiplex["brutal"])
	}
	if up, _ := brutal["up_mbps"].(int); up != 100 {
		t.Fatalf("expected brutal.up_mbps=100, got %#v", brutal["up_mbps"])
	}
	if down, _ := brutal["down_mbps"].(int); down != 200 {
		t.Fatalf("expected brutal.down_mbps=200, got %#v", brutal["down_mbps"])
	}
}

func TestNormalizeInboundMultiplex_InvalidBrutalValue(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": true,
			"brutal": map[string]interface{}{
				"enabled":   true,
				"up_mbps":   0,
				"down_mbps": 100,
			},
		},
	}

	if err := normalizeInboundMultiplex(inbound); err == nil {
		t.Fatalf("expected error for invalid brutal values")
	}
}
