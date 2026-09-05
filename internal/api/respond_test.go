package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErr(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, http.StatusBadRequest, "Invalid request body")

	rec2 := httptest.NewRecorder()
	http.Error(rec2, "Invalid request body", http.StatusBadRequest)

	if rec.Code != rec2.Code {
		t.Fatalf("writeErr status = %d; want %d", rec.Code, rec2.Code)
	}
	if rec.Body.String() != rec2.Body.String() {
		t.Fatalf("writeErr body = %q; want %q", rec.Body.String(), rec2.Body.String())
	}
	if rec.Header().Get("Content-Type") != rec2.Header().Get("Content-Type") {
		t.Fatalf("writeErr Content-Type = %q; want %q", rec.Header().Get("Content-Type"), rec2.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != rec2.Header().Get("X-Content-Type-Options") {
		t.Fatalf("writeErr X-Content-Type-Options = %q; want %q", rec.Header().Get("X-Content-Type-Options"), rec2.Header().Get("X-Content-Type-Options"))
	}
}

func TestWriteJSON_DefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"a": "b"})

	if rec.Code != http.StatusOK {
		t.Fatalf("writeJSON status = %d; want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("writeJSON Content-Type = %q; want application/json", got)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if out["a"] != "b" {
		t.Fatalf("writeJSON body = %v; want map[a:b]", out)
	}
}

func TestWriteJSON_CreatedStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, struct {
		X int `json:"x"`
	}{1})

	if rec.Code != http.StatusCreated {
		t.Fatalf("writeJSON status = %d; want %d", rec.Code, http.StatusCreated)
	}
	var out struct {
		X int `json:"x"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if out.X != 1 {
		t.Fatalf("writeJSON body X = %d; want 1", out.X)
	}
}

func TestWriteJSON_EncodingMatchesEncoder(t *testing.T) {
	v := map[string]int{"n": 42}

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, v)

	var buf strings.Builder
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("json.NewEncoder.Encode: %v", err)
	}

	if rec.Body.String() != buf.String() {
		t.Fatalf("writeJSON body = %q; want %q (matching json.Encoder output incl. trailing newline)", rec.Body.String(), buf.String())
	}
}

func TestDecodeJSON_Valid(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"alice"}`))

	var dst struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(req, &dst); err != nil {
		t.Fatalf("decodeJSON error = %v; want nil", err)
	}
	if dst.Name != "alice" {
		t.Fatalf("decodeJSON dst.Name = %q; want alice", dst.Name)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("decodeJSON wrote to ResponseWriter body: %q", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("decodeJSON caused status write: %d; want default 200", rec.Code)
	}
}

func TestDecodeJSON_Invalid(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{invalid`))

	var dst struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(req, &dst); err == nil {
		t.Fatalf("decodeJSON error = nil; want non-nil for malformed JSON")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("decodeJSON wrote to ResponseWriter body: %q", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("decodeJSON caused status write: %d; want default 200", rec.Code)
	}
}
