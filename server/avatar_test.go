package main

import (
	"bytes"
	"encoding/json"
	"image"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := imgpng.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStoreAvatarRoundtrip(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.store.GetAvatar("max"); err != ErrNotFound {
		t.Fatalf("want not found, got %v", err)
	}
	if v, _ := a.store.AvatarVersion("max"); v != 0 {
		t.Fatalf("version %d", v)
	}
	v, err := a.store.SaveAvatar("max", "image/png", []byte("one"))
	if err != nil || v == 0 {
		t.Fatalf("save %d %v", v, err)
	}
	if _, err := a.store.SaveAvatar("max", "image/jpeg", []byte("two")); err != nil {
		t.Fatal(err)
	}
	av, err := a.store.GetAvatar("max")
	if err != nil || av.Mime != "image/jpeg" || string(av.Data) != "two" {
		t.Fatalf("got %+v %v", av, err)
	}
	if _, err := a.store.GetAvatar("boss"); err != ErrNotFound {
		t.Fatalf("other user: %v", err)
	}
	if err := a.store.DeleteAvatar("max"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.GetAvatar("max"); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func avatarReq(t *testing.T, a *App, c *http.Cookie, method string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/avatar", bytes.NewReader(body))
	req.AddCookie(c)
	return do(t, a.Routes(), req)
}

func TestAvatarHandlers(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	if rec := avatarReq(t, a, max, "GET", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("empty get %d", rec.Code)
	}
	img := pngBytes(t, 128, 128)
	rec := avatarReq(t, a, max, "PUT", img)
	if rec.Code != 200 {
		t.Fatalf("put %d %s", rec.Code, rec.Body)
	}
	var out struct{ Avatar int64 }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Avatar == 0 {
		t.Fatal("no version")
	}
	rec = avatarReq(t, a, max, "GET", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), img) {
		t.Fatalf("get %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	etag := rec.Header().Get("ETag")
	req := httptest.NewRequest("GET", "/api/avatar", nil)
	req.AddCookie(max)
	req.Header.Set("If-None-Match", etag)
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusNotModified {
		t.Fatalf("etag %d", rec.Code)
	}
	if rec := avatarReq(t, a, boss, "GET", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("isolation %d", rec.Code)
	}
	me := httptest.NewRequest("GET", "/api/me", nil)
	me.AddCookie(max)
	var meOut struct{ Avatar int64 }
	json.Unmarshal(do(t, a.Routes(), me).Body.Bytes(), &meOut)
	if meOut.Avatar != out.Avatar {
		t.Fatalf("me avatar %d want %d", meOut.Avatar, out.Avatar)
	}
	if rec := avatarReq(t, a, max, "DELETE", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete %d", rec.Code)
	}
	if rec := avatarReq(t, a, max, "GET", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("after delete %d", rec.Code)
	}
}

func TestAvatarRejects(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	cases := []struct {
		name string
		body []byte
		want int
	}{
		{"text", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), http.StatusUnsupportedMediaType},
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), http.StatusUnsupportedMediaType},
		{"huge dims", pngBytes(t, 2000, 10), http.StatusUnsupportedMediaType},
		{"too big", append(pngBytes(t, 8, 8), make([]byte, maxAvatarBytes)...), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		if rec := avatarReq(t, a, c, "PUT", tc.body); rec.Code != tc.want {
			t.Errorf("%s: status %d want %d", tc.name, rec.Code, tc.want)
		}
	}
	if _, err := a.store.GetAvatar("max"); err != ErrNotFound {
		t.Fatalf("stored after reject: %v", err)
	}
	req := httptest.NewRequest("PUT", "/api/avatar", bytes.NewReader(pngBytes(t, 8, 8)))
	req.AddCookie(c)
	req.Header.Set("Origin", "http://evil.example")
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin %d", rec.Code)
	}
}
