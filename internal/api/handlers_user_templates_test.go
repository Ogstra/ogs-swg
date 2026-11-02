package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func templateCall(srv *Server, handler http.HandlerFunc, method, path, id string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestUserTemplates_CRUD(t *testing.T) {
	srv, _ := newNtfyTestServerWithStore(t)

	create := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: " Basic ", InboundTag: "in-reality", Flow: "xtls-rprx-vision", QuotaLimit: 50 << 30, QuotaPeriod: "monthly"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%q", create.Code, create.Body.String())
	}
	var created core.UserTemplate
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Basic" || created.ID == 0 {
		t.Fatalf("created = %#v", created)
	}

	dup := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "", core.UserTemplate{Name: "basic"})
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate name status=%d; want 409", dup.Code)
	}
	for name, tmpl := range map[string]core.UserTemplate{
		"empty name":     {Name: "  "},
		"negative quota": {Name: "x", QuotaLimit: -1},
		"bad period":     {Name: "x", QuotaPeriod: "weekly"},
	} {
		if rec := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "", tmpl); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d; want 400", name, rec.Code)
		}
	}

	id := strconv.FormatInt(created.ID, 10)
	upd := templateCall(srv, srv.handleUpdateUserTemplate, http.MethodPut, "/api/user-templates/"+id, id,
		core.UserTemplate{Name: "Premium", QuotaLimit: 200 << 30, QuotaPeriod: "total"})
	if upd.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", upd.Code, upd.Body.String())
	}
	if missing := templateCall(srv, srv.handleUpdateUserTemplate, http.MethodPut, "/api/user-templates/999", "999", core.UserTemplate{Name: "Z"}); missing.Code != http.StatusNotFound {
		t.Fatalf("update of a missing template status=%d; want 404", missing.Code)
	}

	list := templateCall(srv, srv.handleListUserTemplates, http.MethodGet, "/api/user-templates", "", nil)
	var templates []core.UserTemplate
	if err := json.Unmarshal(list.Body.Bytes(), &templates); err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 || templates[0].Name != "Premium" || templates[0].QuotaPeriod != "total" {
		t.Fatalf("templates = %#v", templates)
	}

	if rec := templateCall(srv, srv.handleDeleteUserTemplate, http.MethodDelete, "/api/user-templates/"+id, id, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", rec.Code)
	}
	if rec := templateCall(srv, srv.handleDeleteUserTemplate, http.MethodDelete, "/api/user-templates/"+id, id, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d; want 404", rec.Code)
	}
}

func TestUserTemplates_RouteTagsFollowTheTagLifecycle(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	keep, err := st.CreateUserRouteTag("Streaming", "#fff", "", `{"auth_user":["x"]}`)
	if err != nil {
		t.Fatal(err)
	}
	drop, err := st.CreateUserRouteTag("Blocked", "#000", "", `{"auth_user":["y"]}`)
	if err != nil {
		t.Fatal(err)
	}

	create := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: "Tagged", RouteTagIDs: []int64{keep.ID, drop.ID, keep.ID}})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%q", create.Code, create.Body.String())
	}
	var created core.UserTemplate
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	if rec := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: "Bad", RouteTagIDs: []int64{9999}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown route tag status=%d; want 400", rec.Code)
	}

	list := func() []core.UserTemplate {
		rec := templateCall(srv, srv.handleListUserTemplates, http.MethodGet, "/api/user-templates", "", nil)
		var out []core.UserTemplate
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := list()[0].RouteTagIDs; len(got) != 2 {
		t.Fatalf("route tags = %v; want the two distinct ids", got)
	}

	// Renaming a tag changes nothing in the template: it points at the id.
	if err := st.UpdateUserRouteTag(core.UserRouteTag{ID: keep.ID, Name: "Streaming 4K", Color: "#fff", RuleMatchJSON: keep.RuleMatchJSON}); err != nil {
		t.Fatal(err)
	}
	if got := list()[0].RouteTagIDs; len(got) != 2 {
		t.Fatalf("renaming a tag changed the template: %v", got)
	}

	// Deleting a tag removes it from every template.
	if err := st.DeleteUserRouteTag(drop.ID); err != nil {
		t.Fatal(err)
	}
	if got := list()[0].RouteTagIDs; len(got) != 1 || got[0] != keep.ID {
		t.Fatalf("after deleting a tag the template has %v; want only %d", got, keep.ID)
	}
}

func TestUserTemplates_ExpiryFields(t *testing.T) {
	srv, _ := newNtfyTestServerWithStore(t)
	create := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: "Trial", ExpireDays: 7, ExpireOnFirstUse: true})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%q", create.Code, create.Body.String())
	}
	if rec := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: "Forever+", ExpireDays: 99999}); rec.Code != http.StatusBadRequest {
		t.Fatalf("absurd expiry status=%d; want 400", rec.Code)
	}
	if rec := templateCall(srv, srv.handleCreateUserTemplate, http.MethodPost, "/api/user-templates", "",
		core.UserTemplate{Name: "Negative", ExpireDays: -1}); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative expiry status=%d; want 400", rec.Code)
	}
	list := templateCall(srv, srv.handleListUserTemplates, http.MethodGet, "/api/user-templates", "", nil)
	var got []core.UserTemplate
	if err := json.Unmarshal(list.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ExpireDays != 7 || !got[0].ExpireOnFirstUse {
		t.Fatalf("templates = %#v", got)
	}
}
