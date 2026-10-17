package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// newMigrationAdminFixture is a golden server with the moving device already
// known on the source sub, as it is after its first request.
func newMigrationAdminFixture(t *testing.T) *migrationFixture {
	t.Helper()
	f := newMigrationFixture(t, nil)
	// The fixture arms a migration; start from a clean slate.
	if _, err := f.store.CancelAllHWIDMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.RecordSubscriptionHWIDFirstSeen(context.Background(), f.srcID, f.hash, 10); err != nil {
		t.Fatal(err)
	}
	return f
}

func adminCall(h http.HandlerFunc, method, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/test", strings.NewReader(body))
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func createMigrationBody(hash string, target int64) string {
	raw, _ := json.Marshal(map[string]any{"hwid_hash": hash, "target_sub_id": target})
	return string(raw)
}

func TestAdminCreateHWIDMigration(t *testing.T) {
	f := newMigrationAdminFixture(t)
	src := strconv.FormatInt(f.srcID, 10)
	path := map[string]string{"id": src}

	t.Run("ok", func(t *testing.T) {
		rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), path)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%q", rec.Code, rec.Body.String())
		}
		var m core.HWIDMigration
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Status != "pending" || m.TargetSubID != f.dstID || m.SourceSubID != f.srcID {
			t.Fatalf("migration = %#v", m)
		}
	})

	cases := []struct {
		name string
		body string
		want int
	}{
		{"self target", createMigrationBody(f.hash, f.srcID), http.StatusBadRequest},
		{"missing target", createMigrationBody(f.hash, 0), http.StatusBadRequest},
		{"missing hash", createMigrationBody("", f.dstID), http.StatusBadRequest},
		{"invalid body", "not json", http.StatusBadRequest},
		{"unknown target", createMigrationBody(f.hash, 9999), http.StatusNotFound},
		{"unknown device", createMigrationBody("hash-never-seen", f.dstID), http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, tc.body, path)
			if rec.Code != tc.want {
				t.Fatalf("status = %d want %d body=%q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	t.Run("unknown source", func(t *testing.T) {
		rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), map[string]string{"id": "9999"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d want 404", rec.Code)
		}
	})
}

func TestAdminCancelHWIDMigrationStopsNewURL(t *testing.T) {
	f := newMigrationAdminFixture(t)
	path := map[string]string{"id": strconv.FormatInt(f.srcID, 10)}

	if rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), path); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	if got := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID).Header().Get("new-url"); got == "" {
		t.Fatal("armed migration did not send new-url")
	}

	path["hwid_hash"] = f.hash
	if rec := adminCall(f.server.handleCancelHWIDMigration, http.MethodDelete, "", path); rec.Code != http.StatusNoContent {
		t.Fatalf("cancel status = %d body=%q", rec.Code, rec.Body.String())
	}
	if got := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID).Header().Get("new-url"); got != "" {
		t.Fatalf("new-url %q still sent after cancel", got)
	}
}

func TestAdminCancelAllHWIDMigrations(t *testing.T) {
	f := newMigrationAdminFixture(t)
	path := map[string]string{"id": strconv.FormatInt(f.srcID, 10)}
	if rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), path); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}

	rec := adminCall(f.server.handleCancelAllHWIDMigrations, http.MethodPost, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Cancelled int64 `json:"cancelled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Cancelled != 1 {
		t.Fatalf("body = %q err=%v; want cancelled=1", rec.Body.String(), err)
	}
	if got := hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID).Header().Get("new-url"); got != "" {
		t.Fatalf("new-url %q still sent after cancel-all", got)
	}
}

func TestAdminListSubscriptionDevicesShowsMigrationStatus(t *testing.T) {
	f := newMigrationAdminFixture(t)
	path := map[string]string{"id": strconv.FormatInt(f.srcID, 10)}

	list := func() []subscriptionDeviceResponse {
		t.Helper()
		rec := adminCall(f.server.handleListSubscriptionDevices, http.MethodGet, "", path)
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d body=%q", rec.Code, rec.Body.String())
		}
		var out []subscriptionDeviceResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	devices := list()
	if len(devices) != 1 || devices[0].HWIDHash != f.hash || devices[0].Migration == nil || devices[0].Migration.Status != "cancelled" {
		t.Fatalf("devices = %#v; want the one known device with the cancelled fixture migration", devices)
	}
	if strings.Contains(rec(t, f, path), migrationTestHWID) {
		t.Fatal("device list leaks the raw HWID")
	}

	adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), path)
	if m := list()[0].Migration; m == nil || m.Status != "pending" {
		t.Fatalf("migration = %#v; want pending", m)
	}

	hitSub(t, f.server, migrationTestSrcToken, migrationTestHWID)
	if m := list()[0].Migration; m == nil || m.Status != "delivered" {
		t.Fatalf("migration = %#v; want delivered", m)
	}

	hitSub(t, f.server, migrationTestDstToken, migrationTestHWID)
	if m := list()[0].Migration; m == nil || m.Status != "confirmed" {
		t.Fatalf("migration = %#v; want confirmed", m)
	}
}

func rec(t *testing.T, f *migrationFixture, path map[string]string) string {
	t.Helper()
	return adminCall(f.server.handleListSubscriptionDevices, http.MethodGet, "", path).Body.String()
}

func TestAdminHWIDMigrationRoutesRegisterAndAudit(t *testing.T) {
	f := newMigrationAdminFixture(t)
	f.server.auditStore = newTestAuditStore(t)
	if f.server.Routes() == nil {
		t.Fatal("Routes returned nil")
	}

	create := f.server.AuditLogger("subscription", "hwid_migration_create", f.server.handleCreateHWIDMigration)
	rec := adminCall(create, http.MethodPost, createMigrationBody(f.hash, f.dstID), map[string]string{"id": strconv.FormatInt(f.srcID, 10)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	if e := findAuditEntry(queryAuditEntries(t, f.server.auditStore), "subscription", "hwid_migration_create"); e == nil {
		t.Fatal("create was not audited")
	}
}

func TestAdminCreateHWIDMigrationRejectsRearmingConfirmedMove(t *testing.T) {
	f := newMigrationAdminFixture(t)
	ctx := context.Background()
	if err := f.store.CreateHWIDMigration(ctx, f.srcID, f.hash, f.dstID, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.MarkHWIDMigrationDelivered(ctx, f.srcID, f.hash, 35); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.store.ConfirmHWIDMigration(ctx, f.dstID, f.hash, 40); err != nil || !ok {
		t.Fatalf("ConfirmHWIDMigration = %v, %v", ok, err)
	}
	path := map[string]string{"id": strconv.FormatInt(f.srcID, 10)}
	rec := adminCall(f.server.handleCreateHWIDMigration, http.MethodPost, createMigrationBody(f.hash, f.dstID), path)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d want 409 body=%q", rec.Code, rec.Body.String())
	}
}

func TestAdminDeleteSubscriptionDeviceOnlyWhenMoved(t *testing.T) {
	f := newMigrationAdminFixture(t)
	ctx := context.Background()
	path := map[string]string{"id": strconv.FormatInt(f.srcID, 10), "hwid_hash": f.hash}

	// Not moved yet: refused.
	if rec := adminCall(f.server.handleDeleteSubscriptionDevice, http.MethodDelete, "", path); rec.Code != http.StatusConflict {
		t.Fatalf("status before the move = %d want 409", rec.Code)
	}
	// Armed but not confirmed: still refused.
	if err := f.store.CreateHWIDMigration(ctx, f.srcID, f.hash, f.dstID, 30); err != nil {
		t.Fatal(err)
	}
	if rec := adminCall(f.server.handleDeleteSubscriptionDevice, http.MethodDelete, "", path); rec.Code != http.StatusConflict {
		t.Fatalf("status while pending = %d want 409", rec.Code)
	}
	// Confirmed: removable.
	if _, err := f.store.MarkHWIDMigrationDelivered(ctx, f.srcID, f.hash, 35); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.store.ConfirmHWIDMigration(ctx, f.dstID, f.hash, 40); err != nil || !ok {
		t.Fatalf("ConfirmHWIDMigration = %v, %v", ok, err)
	}
	if rec := adminCall(f.server.handleDeleteSubscriptionDevice, http.MethodDelete, "", path); rec.Code != http.StatusNoContent {
		t.Fatalf("status after the move = %d want 204 body=%q", rec.Code, rec.Body.String())
	}
	if known, _ := f.store.HasSubscriptionKnownHWID(ctx, f.srcID, f.hash); known {
		t.Fatal("removed device is still known on the source sub")
	}
}
