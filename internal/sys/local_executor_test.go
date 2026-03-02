package sys

import "testing"

func TestResolveUnitName_WireGuardDynamicInterface(t *testing.T) {
	if got := resolveUnitName("wireguard"); got != "wg-quick@wg0" {
		t.Fatalf("resolveUnitName(wireguard)=%q, want %q", got, "wg-quick@wg0")
	}
	if got := resolveUnitName("wireguard", "wg1"); got != "wg-quick@wg1" {
		t.Fatalf("resolveUnitName(wireguard,wg1)=%q, want %q", got, "wg-quick@wg1")
	}
	if got := resolveUnitName("wireguard", "wg2.conf"); got != "wg-quick@wg2" {
		t.Fatalf("resolveUnitName(wireguard,wg2.conf)=%q, want %q", got, "wg-quick@wg2")
	}
}
