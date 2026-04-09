package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func TestDashboardPreferencesHandlersPersistPerPrincipal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreatePanelUser("alice", "password123", core.PanelUserPermissions{CanReadSettings: true, CanWriteSettings: true}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	server := NewServer(store, &core.Config{DemoMode: true}, &dashboardExecutorStub{})
	authReq := func(req *http.Request) *http.Request {
		ctx := context.WithValue(req.Context(), userContextKey, map[string]interface{}{"sub": "alice"})
		return req.WithContext(ctx)
	}

	getReq := authReq(httptest.NewRequest(http.MethodGet, "/api/settings/dashboard-preferences", nil))
	getRec := httptest.NewRecorder()
	server.handleGetDashboardPreferences(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("default get status=%d body=%q", getRec.Code, getRec.Body.String())
	}

	var defaults core.DashboardPreferences
	if err := json.NewDecoder(getRec.Body).Decode(&defaults); err != nil {
		t.Fatalf("decode defaults: %v", err)
	}
	if defaults.DefaultService != "singbox" || defaults.RefreshMs != 10000 || defaults.DefaultRange != "24h" || defaults.DetailChartTargetPoints != 200 {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	putReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/dashboard-preferences", strings.NewReader(`{"default_service":"wireguard","refresh_ms":15000,"default_range":"1w","detail_chart_target_points":100}`)))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	server.handleUpdateDashboardPreferences(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", putRec.Code, putRec.Body.String())
	}

	getRec = httptest.NewRecorder()
	server.handleGetDashboardPreferences(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("updated get status=%d body=%q", getRec.Code, getRec.Body.String())
	}

	var saved core.DashboardPreferences
	if err := json.NewDecoder(getRec.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if saved.DefaultService != "wireguard" || saved.RefreshMs != 15000 || saved.DefaultRange != "1w" || saved.DetailChartTargetPoints != 100 {
		t.Fatalf("unexpected saved prefs: %+v", saved)
	}
}
