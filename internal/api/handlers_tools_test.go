package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func withToolPerms(r *http.Request, perms *core.PanelUserPermissions) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), permissionsContextKey, perms))
}

func TestHandleGenerateRandBase64_ReturnsBase64OfRequestedLength(t *testing.T) {
	server := NewServer(nil, &core.Config{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/tools/rand-base64", bytes.NewBufferString(`{"key_length":32}`))
	req.Header.Set("Content-Type", "application/json")
	req = withToolPerms(req, &core.PanelUserPermissions{CanWriteUsers: true})

	rec := httptest.NewRecorder()
	server.handleGenerateRandBase64(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var resp RandBase64Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v body=%q", err, rec.Body.String())
	}
	if resp.Value == "" {
		t.Fatal("expected non-empty value")
	}

	decoded, err := base64.StdEncoding.DecodeString(resp.Value)
	if err != nil {
		t.Fatalf("decode base64: %v value=%q", err, resp.Value)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded length=%d want 32", len(decoded))
	}
}

func TestHandleGenerateRandBase64_RejectsNonPositiveLength(t *testing.T) {
	server := NewServer(nil, &core.Config{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/tools/rand-base64", bytes.NewBufferString(`{"key_length":0}`))
	req.Header.Set("Content-Type", "application/json")
	req = withToolPerms(req, &core.PanelUserPermissions{CanWriteConfig: true})

	rec := httptest.NewRecorder()
	server.handleGenerateRandBase64(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}
