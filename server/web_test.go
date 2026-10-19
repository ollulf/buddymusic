package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticGzipAndETag(t *testing.T) {
	h := newTestApp(t).Routes()
	req := httptest.NewRequest("GET", "/app.css", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip 200, got %d %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	want, _ := webFS.ReadFile("web/app.css")
	if !bytes.Equal(plain, want) {
		t.Fatal("gzip body differs from app.css")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("headers: %v", rec.Header())
	}

	req = httptest.NewRequest("GET", "/app.css", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("want 304, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app.css", nil))
	if rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatal("plain request should get the raw file")
	}
}

func TestStaticVendorCachedAndShellPreloads(t *testing.T) {
	h := newTestApp(t).Routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/vendor/jsmediatags.min.js", nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=") {
		t.Fatalf("vendor: %d %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `<link rel="modulepreload" href="/dom.js">`) || strings.Contains(body, `modulepreload" href="/app.js"`) {
		t.Fatalf("shell: %d %s", rec.Code, body)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("ETag") == "" {
		t.Fatalf("shell headers: %v", rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope.js", nil))
	if rec.Code != 404 {
		t.Fatalf("missing file: %d", rec.Code)
	}
}
