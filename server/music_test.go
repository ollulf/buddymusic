package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMusicAlbumsAndAlbum(t *testing.T) {
	a := newTestApp(t)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/albums?type=recent", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"recent"`) {
		t.Fatalf("albums %d: %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/albums?type=evil", nil))
	if rec.Code != 400 {
		t.Fatalf("bad type status %d", rec.Code)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"song"`) {
		t.Fatalf("album %d: %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/album/missing", nil))
	if rec.Code != 404 {
		t.Fatalf("missing album status %d", rec.Code)
	}
}

func TestMusicRequiresLogin(t *testing.T) {
	a := newTestApp(t)
	rec := do(t, a.Routes(), httptest.NewRequest("GET", "/api/music/artists", nil))
	if rec.Code != 401 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMusicStreamRange(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/api/music/stream/s1", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := authed(t, a, req)
	if rec.Code != 206 || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range %d %q %q", rec.Code, rec.Body, rec.Header().Get("Content-Range"))
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=999", nil))
	if rec.Code != 200 || rec.Body.String() != "0123456789opus128" {
		t.Fatalf("opus %d %q", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/..%2Fx", nil))
	if rec.Code != 400 {
		t.Fatalf("bad id status %d", rec.Code)
	}
}
