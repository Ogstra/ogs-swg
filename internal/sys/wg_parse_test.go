package sys

import "testing"

func TestParseWGDumpStats_PopulatesInterfaceName(t *testing.T) {
	pub := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN0123456789+/"
	dump := "wg1 " + pub + " (none) 198.51.100.2:51821 10.0.0.2/32 1700000000 1024 2048 25\n"

	stats := parseWGDumpStats([]byte(dump))
	st, ok := stats[pub]
	if !ok {
		t.Fatalf("peer %q not parsed", pub)
	}
	if st.InterfaceName != "wg1" {
		t.Fatalf("InterfaceName=%q, want %q", st.InterfaceName, "wg1")
	}
	if st.TransferRx != 1024 || st.TransferTx != 2048 {
		t.Fatalf("unexpected transfer values: rx=%d tx=%d", st.TransferRx, st.TransferTx)
	}
}

func TestParseWGTextStats_PopulatesInterfaceName(t *testing.T) {
	pub := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN0123456789+/"
	text := "" +
		"interface: wg2\n" +
		"  public key: server-key\n" +
		"peer: " + pub + "\n" +
		"  endpoint: 203.0.113.10:51820\n" +
		"  latest handshake: 2 minutes, 5 seconds ago\n" +
		"  transfer: 1.00 KiB received, 2.00 KiB sent\n"

	stats := parseWGTextStats([]byte(text))
	st, ok := stats[pub]
	if !ok {
		t.Fatalf("peer %q not parsed", pub)
	}
	if st.InterfaceName != "wg2" {
		t.Fatalf("InterfaceName=%q, want %q", st.InterfaceName, "wg2")
	}
	if st.Endpoint != "203.0.113.10:51820" {
		t.Fatalf("Endpoint=%q", st.Endpoint)
	}
	if st.TransferRx != 1024 || st.TransferTx != 2048 {
		t.Fatalf("unexpected transfer values: rx=%d tx=%d", st.TransferRx, st.TransferTx)
	}
	if st.LatestHandshake <= 0 {
		t.Fatalf("LatestHandshake should be populated, got %d", st.LatestHandshake)
	}
}
