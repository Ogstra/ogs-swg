package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaticAssets_AreCompressedOnceAndServedPlainWhenNotAccepted(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := strings.Repeat("console.log('panel');\n", 400)
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	router := http.NewServeMux()
	registerFrontendRoutes(router, dir)

	get := func(acceptGzip bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		if acceptGzip {
			req.Header.Set("Accept-Encoding", "gzip, deflate")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	gz := get(true)
	if gz.Code != http.StatusOK || gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("status=%d encoding=%q", gz.Code, gz.Header().Get("Content-Encoding"))
	}
	if ct := gz.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content type = %q", ct)
	}
	if cc := gz.Header().Get("Cache-Control"); cc != frontendAssetCacheControl {
		t.Fatalf("cache control = %q", cc)
	}
	reader, err := gzip.NewReader(bytes.NewReader(gz.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := io.ReadAll(reader)
	if string(decoded) != original {
		t.Fatal("the compressed body does not decode to the original file")
	}
	if gz.Body.Len() >= len(original)/2 {
		t.Fatalf("compressed %d of %d bytes; expected much smaller", gz.Body.Len(), len(original))
	}

	// A second request reuses the stored bytes.
	again := get(true)
	if !bytes.Equal(again.Body.Bytes(), gz.Body.Bytes()) {
		t.Fatal("the second compressed response differs from the first")
	}

	plain := get(false)
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != original {
		t.Fatalf("a client without gzip must get the file as is (encoding=%q)", plain.Header().Get("Content-Encoding"))
	}

	// A changed file is recompressed.
	updated := strings.Repeat("console.log('changed');\n", 400)
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "assets", "app.js"), later, later); err != nil {
		t.Fatal(err)
	}
	reader, _ = gzip.NewReader(bytes.NewReader(get(true).Body.Bytes()))
	decoded, _ = io.ReadAll(reader)
	if string(decoded) != updated {
		t.Fatal("a changed file kept being served from the old compressed copy")
	}
}
