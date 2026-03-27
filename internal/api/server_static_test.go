package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterFrontendRoutesSetsNoCacheForSPAFallback(t *testing.T) {
	distDir := writeFrontendFixture(t)

	mux := http.NewServeMux()
	registerFrontendRoutes(mux, distDir)

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != frontendDocumentCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, frontendDocumentCacheControl)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want %q", got, "no-cache")
	}
	if got := rec.Header().Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q, want %q", got, "0")
	}
	if body := rec.Body.String(); body != "<!doctype html><title>test</title>" {
		t.Fatalf("body = %q, want index.html contents", body)
	}
}

func TestRegisterFrontendRoutesSetsImmutableCacheForAssets(t *testing.T) {
	distDir := writeFrontendFixture(t)

	mux := http.NewServeMux()
	registerFrontendRoutes(mux, distDir)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != frontendAssetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, frontendAssetCacheControl)
	}
	if body := rec.Body.String(); body != "console.log('ok');" {
		t.Fatalf("body = %q, want asset contents", body)
	}
}

func writeFrontendFixture(t *testing.T) string {
	t.Helper()

	distDir := t.TempDir()
	assetDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html><title>test</title>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "app.js"), []byte("console.log('ok');"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	return distDir
}
