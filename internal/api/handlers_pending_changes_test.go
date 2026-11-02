package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSingboxPendingChangesEndpointIsALightFlag(t *testing.T) {
	srv, _ := newNtfyTestServerWithStore(t)
	rec := httptest.NewRecorder()
	srv.handleGetSingboxPendingChanges(rec, httptest.NewRequest(http.MethodGet, "/api/singbox/pending-changes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var got map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["singbox_pending_changes"]; !ok || v {
		t.Fatalf("response = %v; want singbox_pending_changes=false", got)
	}
	if len(got) != 1 {
		t.Fatalf("the response must carry only the flag, got %v", got)
	}
}
