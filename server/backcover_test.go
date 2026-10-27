package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadBackCover(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	one := makeAudio(t, "a.flac", true, "title", "Intro", "artist", "Test Band", "album", "Flip", "track", "1")
	back := jpeg(t)
	rec, _ := postUpload(t, a, cookie, formPart{"back", "b.jpg", back}, formPart{"file", "a.flac", one})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	got, err := os.ReadFile(filepath.Join(a.cfg.LibraryDir(), "max/Test Band/Flip/back.jpg"))
	if err != nil || !bytes.Equal(got, back) {
		t.Fatalf("back cover not written: %v", err)
	}
	plain := makeAudio(t, "b.flac", true, "title", "Intro", "artist", "Test Band", "album", "Plain", "track", "1")
	if rec, _ := postUpload(t, a, cookie, formPart{"file", "b.flac", plain}); rec.Code != http.StatusCreated {
		t.Fatalf("plain upload: %d %s", rec.Code, rec.Body)
	}
	if f := backCoverFile(filepath.Join(a.cfg.LibraryDir(), "max/Test Band/Plain")); f != "" {
		t.Fatalf("unexpected back cover %s", f)
	}
}

func TestUploadBackCoverRejectsNonImage(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	one := makeAudio(t, "a.flac", true, "title", "Intro", "artist", "Test Band", "album", "Flip", "track", "1")
	rec, _ := postUpload(t, a, login(t, a), formPart{"back", "b.txt", []byte("hello")}, formPart{"file", "a.flac", one})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image back: %d %s", rec.Code, rec.Body)
	}
}

func TestBackCoverEndpoint(t *testing.T) {
	a := vinylApp(t)
	cookie := login(t, a)
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/music/back/al1", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, req)
		return rec
	}
	if rec := get(); rec.Code != http.StatusNotFound {
		t.Fatalf("no back: %d", rec.Code)
	}
	if strings.Contains(authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil)).Body.String(), `"hasBack"`) {
		t.Fatal("hasBack without file")
	}
	back := jpeg(t)
	if err := a.SetBackCover(context.Background(), "max/Old/Disc", back); err != nil {
		t.Fatal(err)
	}
	rec := get()
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), back) || rec.Header().Get("Content-Type") != "image/jpeg" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age") {
		t.Fatalf("back: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil)).Body.String(), `"hasBack":true`) {
		t.Fatal("album lacks hasBack")
	}
}

func TestSetBackCoverOutsideLibrary(t *testing.T) {
	a := vinylApp(t)
	for _, f := range []string{"", "..", "../x", "max/../../etc", "nope/missing"} {
		if err := a.SetBackCover(context.Background(), f, jpeg(t)); err == nil {
			t.Fatalf("folder %q accepted", f)
		}
	}
}

func backReq(t *testing.T, a *App, cookie *http.Cookie, method string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/admin/album/al1/back", bytes.NewReader(body))
	req.AddCookie(cookie)
	return do(t, a.Routes(), req)
}

func TestAlbumBackAdminOnly(t *testing.T) {
	a := vinylApp(t)
	cookie := login(t, a)
	if rec := backReq(t, a, cookie, "PUT", jpeg(t)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin put: %d %s", rec.Code, rec.Body)
	}
	if f := backCoverFile(filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc")); f != "" {
		t.Fatalf("non-admin wrote %s", f)
	}
	if rec := backReq(t, a, cookie, "DELETE", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin delete: %d %s", rec.Code, rec.Body)
	}
}

func TestAlbumBackReplaceAndRemove(t *testing.T) {
	a := vinylApp(t)
	boss := loginAs(t, a, "boss", "bosspw")
	dir := filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc")
	back := jpeg(t)
	if rec := backReq(t, a, boss, "PUT", back); rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	got, err := os.ReadFile(filepath.Join(dir, "back.jpg"))
	if err != nil || !bytes.Equal(got, back) {
		t.Fatalf("back not written: %v", err)
	}
	if !strings.Contains(authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil)).Body.String(), `"backV":`) {
		t.Fatal("album lacks backV")
	}
	if rec := backReq(t, a, boss, "DELETE", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if f := backCoverFile(dir); f != "" {
		t.Fatalf("back still there: %s", f)
	}
	if strings.Contains(authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil)).Body.String(), `"hasBack"`) {
		t.Fatal("hasBack after delete")
	}
}

func TestAlbumBackRejectsNonImage(t *testing.T) {
	a := vinylApp(t)
	boss := loginAs(t, a, "boss", "bosspw")
	if rec := backReq(t, a, boss, "PUT", []byte("hello")); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-image: %d %s", rec.Code, rec.Body)
	}
	if f := backCoverFile(filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc")); f != "" {
		t.Fatalf("non-image wrote %s", f)
	}
}
