package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type dashboardExecutorStub struct {
	stats map[string]core.PeerStats
}

func (s *dashboardExecutorStub) RestartService(context.Context, string) error { return nil }
func (s *dashboardExecutorStub) StartService(context.Context, string) error   { return nil }
func (s *dashboardExecutorStub) StopService(context.Context, string) error    { return nil }
func (s *dashboardExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return true, nil
}
func (s *dashboardExecutorStub) WriteConfig(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (s *dashboardExecutorStub) ReadConfig(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}
func (s *dashboardExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }
func (s *dashboardExecutorStub) GetSysctl(context.Context, string) (string, error) { return "", nil }
func (s *dashboardExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (s *dashboardExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (s *dashboardExecutorStub) SyncWireGuard(context.Context, string, []byte) error { return nil }
func (s *dashboardExecutorStub) RestartWireGuard(context.Context, string) error      { return nil }
func (s *dashboardExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return []string{"wg0", "wg1"}, nil
}
func (s *dashboardExecutorStub) EnableWireGuardInterface(context.Context, string) error  { return nil }
func (s *dashboardExecutorStub) DisableWireGuardInterface(context.Context, string) error { return nil }
func (s *dashboardExecutorStub) ValidateSingboxConfig(context.Context, []byte) error     { return nil }
func (s *dashboardExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return s.stats, nil
}
func (s *dashboardExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *dashboardExecutorStub) Close() error                            { return nil }
func (s *dashboardExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func TestDashboard_WireGuardInterfaceBreakdown(t *testing.T) {
	server, _, _ := newDashboardTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardData(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wgTotal := payload.StatsCards["wireguard"]
	if wgTotal.Uplink != 800 || wgTotal.Downlink != 400 {
		t.Fatalf("wireguard total got uplink=%d downlink=%d", wgTotal.Uplink, wgTotal.Downlink)
	}

	wg0 := payload.WireGuardInterfaces["wg0"]
	wg1 := payload.WireGuardInterfaces["wg1"]
	if wg0.Uplink != 600 || wg0.Downlink != 300 {
		t.Fatalf("wg0 breakdown got uplink=%d downlink=%d", wg0.Uplink, wg0.Downlink)
	}
	if wg1.Uplink != 200 || wg1.Downlink != 100 {
		t.Fatalf("wg1 breakdown got uplink=%d downlink=%d", wg1.Uplink, wg1.Downlink)
	}

	// Chart remains combined for WireGuard mode.
	if len(payload.ChartData) == 0 {
		t.Fatalf("expected non-empty chart data")
	}
	last := payload.ChartData[len(payload.ChartData)-1]
	if last.UpWG != wgTotal.Uplink || last.DownWG != wgTotal.Downlink {
		t.Fatalf("combined chart mismatch: up_wg=%d down_wg=%d total_up=%d total_down=%d", last.UpWG, last.DownWG, wgTotal.Uplink, wgTotal.Downlink)
	}
}

func TestDashboard_TopConsumersIncludeInterfaceLabel(t *testing.T) {
	server, keyWG0, keyWG1 := newDashboardTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardData(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	topWG := payload.TopConsumers["wireguard"]
	if len(topWG) < 2 {
		t.Fatalf("expected at least 2 wireguard consumers, got %d", len(topWG))
	}

	foundWG0 := false
	foundWG1 := false
	for _, c := range topWG {
		if c.Key == keyWG0 {
			foundWG0 = true
			if c.Interface != "wg0" || c.Flow != "WireGuard:wg0" {
				t.Fatalf("wg0 consumer interface/flow mismatch: interface=%q flow=%q", c.Interface, c.Flow)
			}
		}
		if c.Key == keyWG1 {
			foundWG1 = true
			if c.Interface != "wg1" || c.Flow != "WireGuard:wg1" {
				t.Fatalf("wg1 consumer interface/flow mismatch: interface=%q flow=%q", c.Interface, c.Flow)
			}
		}
	}
	if !foundWG0 || !foundWG1 {
		t.Fatalf("missing expected wireguard consumers: foundWG0=%v foundWG1=%v", foundWG0, foundWG1)
	}
}

func TestDashboard_SingboxTopConsumersIncludeCompressedHistoryAfterRename(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()
	oldRawTs := now - 72*3600
	recentRawTs := now - 1800

	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: oldRawTs, Uplink: 120, Downlink: 240},
		{User: "alice", Timestamp: recentRawTs, Uplink: 30, Downlink: 60},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice",
		QuotaLimit:  1024,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	if err := store.CompressOldSamples(now - 24*3600); err != nil {
		t.Fatalf("CompressOldSamples: %v", err)
	}

	if err := store.RenameUserTrafficIdentity("alice", "alice-renamed"); err != nil {
		t.Fatalf("RenameUserTrafficIdentity: %v", err)
	}

	server := NewServer(store, &core.Config{
		EnableSingbox:        true,
		ActiveThresholdBytes: 1,
		DemoMode:             true,
	}, &dashboardExecutorStub{})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?start=1&end="+strconv.FormatInt(now, 10), nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardData(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	topSB := payload.TopConsumers["singbox"]
	if len(topSB) != 1 {
		t.Fatalf("expected exactly 1 singbox consumer, got %d (%+v)", len(topSB), topSB)
	}

	consumer := topSB[0]
	if consumer.Name != "alice-renamed" {
		t.Fatalf("consumer name = %q; want %q", consumer.Name, "alice-renamed")
	}
	if consumer.Key != "alice-renamed" {
		t.Fatalf("consumer key = %q; want %q", consumer.Key, "alice-renamed")
	}
	if consumer.Total != 450 {
		t.Fatalf("consumer total = %d; want 450", consumer.Total)
	}
	if consumer.QuotaLimit != 1024 {
		t.Fatalf("consumer quota_limit = %d; want 1024", consumer.QuotaLimit)
	}
	for _, stale := range topSB {
		if stale.Name == "alice" || stale.Key == "alice" {
			t.Fatalf("stale old-name consumer leaked into dashboard response: %+v", stale)
		}
	}
}

func TestDashboard_ConsumerChartLoadsSingboxUserOnDemand(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: 100, Uplink: 100, Downlink: 50},
		{User: "alice", Timestamp: 160, Uplink: 25, Downlink: 75},
		{User: "bob", Timestamp: 100, Uplink: 999, Downlink: 999},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	server := NewServer(store, &core.Config{
		EnableSingbox: true,
		DemoMode:      true,
	}, &dashboardExecutorStub{})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/consumer-chart?mode=singbox&key=alice&start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardConsumerChart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardConsumerChartData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ChartData) == 0 {
		t.Fatalf("expected chart data")
	}
	last := payload.ChartData[len(payload.ChartData)-1]
	if last.UpSB != 125 || last.DownSB != 125 {
		t.Fatalf("last singbox point got up_sb=%d down_sb=%d", last.UpSB, last.DownSB)
	}
}

func TestDashboard_ConsumerChartLoadsWireGuardPeerOnDemand(t *testing.T) {
	server, keyWG0, _ := newDashboardTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/consumer-chart?mode=wireguard&key="+keyWG0+"&start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardConsumerChart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardConsumerChartData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ChartData) == 0 {
		t.Fatalf("expected chart data")
	}
	last := payload.ChartData[len(payload.ChartData)-1]
	if last.UpWG != 600 || last.DownWG != 300 {
		t.Fatalf("last wireguard point got up_wg=%d down_wg=%d", last.UpWG, last.DownWG)
	}
}

func TestDashboard_ConsumerChartResolvesMaskedSingboxKeyByName(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: 100, Uplink: 80, Downlink: 20},
		{User: "alice", Timestamp: 160, Uplink: 20, Downlink: 40},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	server := NewServer(store, &core.Config{
		EnableSingbox: true,
		DemoMode:      true,
	}, &dashboardExecutorStub{})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/consumer-chart?mode=singbox&key=********&name=alice&start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardConsumerChart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDashboard_ConsumerChartResolvesMaskedWireGuardKeyByAliasAndInterface(t *testing.T) {
	server, _, _ := newDashboardTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/consumer-chart?mode=wireguard&key=********&name=alice&interface_name=wg0&start=100&end=200", nil)
	rec := httptest.NewRecorder()
	server.handleGetDashboardConsumerChart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload DashboardConsumerChartData
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	last := payload.ChartData[len(payload.ChartData)-1]
	if last.UpWG != 600 || last.DownWG != 300 {
		t.Fatalf("last wireguard point got up_wg=%d down_wg=%d", last.UpWG, last.DownWG)
	}
}

func newDashboardTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	keyWG0 := "peer-wg0-public-key"
	keyWG1 := "peer-wg1-public-key"

	wg0Path := filepath.Join(tmp, "wg0.conf")
	wg1Path := filepath.Join(tmp, "wg1.conf")
	wg0Config := "[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n\n[Peer]\n# Alias = alice\nPublicKey = " + keyWG0 + "\nAllowedIPs = 10.0.0.2/32\n"
	wg1Config := "[Interface]\nAddress = 10.1.0.1/24\nListenPort = 51821\n\n[Peer]\n# Alias = bob\nPublicKey = " + keyWG1 + "\nAllowedIPs = 10.1.0.2/32\n"
	if err := os.WriteFile(wg0Path, []byte(wg0Config), 0644); err != nil {
		t.Fatalf("write wg0.conf: %v", err)
	}
	if err := os.WriteFile(wg1Path, []byte(wg1Config), 0644); err != nil {
		t.Fatalf("write wg1.conf: %v", err)
	}

	_ = store.UpsertWGPeer(keyWG0, "alice", false)
	_ = store.UpsertWGPeer(keyWG1, "bob", false)
	samples := []core.WGSample{
		{PublicKey: keyWG0, Timestamp: 100, Rx: 100, Tx: 100},
		{PublicKey: keyWG0, Timestamp: 200, Rx: 400, Tx: 700},
		{PublicKey: keyWG1, Timestamp: 100, Rx: 50, Tx: 50},
		{PublicKey: keyWG1, Timestamp: 200, Rx: 150, Tx: 250},
	}
	if err := store.InsertWGSamples(samples); err != nil {
		t.Fatalf("InsertWGSamples: %v", err)
	}

	now := time.Now().Unix()
	exec := &dashboardExecutorStub{
		stats: map[string]core.PeerStats{
			keyWG0: {PublicKey: keyWG0, InterfaceName: "wg0", LatestHandshake: now},
			keyWG1: {PublicKey: keyWG1, InterfaceName: "wg1", LatestHandshake: now},
		},
	}
	cfg := &core.Config{
		EnableWireGuard:     true,
		EnableSingbox:       false,
		WireGuardConfigPath: wg0Path,
		WireGuardConfigDir:  tmp,
	}

	return NewServer(store, cfg, exec), keyWG0, keyWG1
}
