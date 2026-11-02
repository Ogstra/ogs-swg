package api

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Compressing a 300 KB script on every request wastes CPU, and a proxy in front
// does not always do it. Static files are compressed once and the result is
// reused until the file changes.

type gzippedFile struct {
	modTime time.Time
	size    int64
	data    []byte
}

var staticGzipCache sync.Map // file path -> *gzippedFile

func compressibleStaticExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".js", ".mjs", ".css", ".html", ".svg", ".json", ".webmanifest", ".map", ".txt", ".xml":
		return true
	}
	return false
}

// serveStaticGzip serves fullPath compressed when the client accepts gzip and
// the file type benefits from it. It reports false when it did nothing, so the
// caller falls back to plain serving.
func serveStaticGzip(w http.ResponseWriter, r *http.Request, fullPath string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Header.Get("Range") != "" {
		return false
	}
	ext := filepath.Ext(fullPath)
	if !compressibleStaticExt(ext) {
		return false
	}
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() || info.Size() < 1024 {
		return false
	}

	var entry *gzippedFile
	if cached, ok := staticGzipCache.Load(fullPath); ok {
		if candidate := cached.(*gzippedFile); candidate.modTime.Equal(info.ModTime()) && candidate.size == info.Size() {
			entry = candidate
		}
	}
	if entry == nil {
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return false
		}
		var buf bytes.Buffer
		gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := gz.Write(raw); err != nil || gz.Close() != nil {
			return false
		}
		entry = &gzippedFile{modTime: info.ModTime(), size: info.Size(), data: buf.Bytes()}
		staticGzipCache.Store(fullPath, entry)
	}

	if contentType := mime.TypeByExtension(ext); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	http.ServeContent(w, r, filepath.Base(fullPath), entry.modTime, bytes.NewReader(entry.data))
	return true
}
