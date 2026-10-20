package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
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
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=80", nil))
	if rec.Code != 200 || rec.Body.String() != "0123456789opus80" {
		t.Fatalf("opus 80 %d %q", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3", nil))
	if rec.Code != 200 || rec.Body.String() != "0123456789mp3320" {
		t.Fatalf("mp3 %d %q", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=wav", nil))
	if rec.Code != 400 {
		t.Fatalf("bad format status %d", rec.Code)
	}
	for _, f := range []string{"format=opus&maxBitRate=80", "format=mp3&maxBitRate=320"} {
		req := httptest.NewRequest("GET", "/api/music/stream/s1?"+f, nil)
		req.Header.Set("Range", "bytes=2-5")
		rec = authed(t, a, req)
		if rec.Code != 206 || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" || rec.Header().Get("Accept-Ranges") != "bytes" {
			t.Fatalf("transcoded range %s %d %q", f, rec.Code, rec.Body)
		}
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/..%2Fx", nil))
	if rec.Code != 400 {
		t.Fatalf("bad id status %d", rec.Code)
	}
}

func TestStreamParamsWhitelist(t *testing.T) {
	cases := []struct {
		in, format, rate string
		ok               bool
	}{
		{"", "", "", true},
		{"format=opus&maxBitRate=80", "opus", "80", true},
		{"format=opus&maxBitRate=31", "opus", "128", true},
		{"format=opus&maxBitRate=320", "opus", "320", true},
		{"format=mp3&maxBitRate=192", "mp3", "192", true},
		{"format=mp3&maxBitRate=321", "mp3", "320", true},
		{"format=flac", "", "", false},
		{"format=raw", "", "", false},
	}
	for _, c := range cases {
		in, _ := url.ParseQuery(c.in)
		q, ok := streamParams("s1", in)
		if ok != c.ok {
			t.Fatalf("%q ok=%v", c.in, ok)
		}
		if ok && (q.Get("format") != c.format || q.Get("maxBitRate") != c.rate || q.Get("id") != "s1") {
			t.Fatalf("%q -> %v", c.in, q)
		}
	}
}

func TestMusicStarUsesOwnLogin(t *testing.T) {
	var calls []string
	a := newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		q := r.URL.Query()
		switch {
		case strings.HasSuffix(r.URL.Path, "/star.view"), strings.HasSuffix(r.URL.Path, "/unstar.view"):
			calls = append(calls, path.Base(r.URL.Path)+" "+q.Get("u")+" id="+q.Get("id")+" albumId="+q.Get("albumId"))
			w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
			return true
		case strings.HasSuffix(r.URL.Path, "/getStarred2.view"):
			w.Write([]byte(`{"subsonic-response":{"status":"ok","starred2":{"song":[{"id":"s1","title":"One","starred":"2026-01-01T00:00:00Z"}],"album":[{"id":"al1","name":"Blue"}]}}}`))
			return true
		}
		return false
	})
	rec := authed(t, a, httptest.NewRequest("POST", "/api/music/star/s1", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"starred":true`) {
		t.Fatalf("star %d: %s", rec.Code, rec.Body)
	}
	req := httptest.NewRequest("DELETE", "/api/music/star/al1?kind=album", nil)
	req.AddCookie(loginAs(t, a, "boss", "bosspw"))
	rec = do(t, a.Routes(), req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"starred":false`) {
		t.Fatalf("unstar %d: %s", rec.Code, rec.Body)
	}
	want := []string{"star.view max id=s1 albumId=", "unstar.view boss id= albumId=al1"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", calls)
	}
	rec = authed(t, a, httptest.NewRequest("POST", "/api/music/star/s1?kind=artist", nil))
	if rec.Code != 400 {
		t.Fatalf("bad kind status %d", rec.Code)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/starred", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"song":[{"id":"s1"`) || !strings.Contains(rec.Body.String(), `"album":[{"id":"al1"`) {
		t.Fatalf("starred %d: %s", rec.Code, rec.Body)
	}
}

func TestMusicStarRequiresLogin(t *testing.T) {
	a := newTestApp(t)
	for _, req := range []*http.Request{
		httptest.NewRequest("POST", "/api/music/star/s1", nil),
		httptest.NewRequest("DELETE", "/api/music/star/s1", nil),
		httptest.NewRequest("GET", "/api/music/starred", nil),
	} {
		if rec := do(t, a.Routes(), req); rec.Code != 401 {
			t.Fatalf("%s %s status %d", req.Method, req.URL, rec.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/music/star/s1", nil)
	req.Header.Set("Origin", "https://evil.example")
	if rec := authed(t, a, req); rec.Code != 403 {
		t.Fatalf("cross-origin status %d", rec.Code)
	}
}
