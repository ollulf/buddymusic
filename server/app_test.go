package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"_incoming", "library", "data"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	cfg := &Config{
		MusicRoot:   root,
		DataDir:     filepath.Join(root, "data"),
		SessionKey:  bytes.Repeat([]byte{7}, 32),
		MaxUpload:   1 << 20,
		TrustedNets: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
	}
	nav := httptest.NewServer(fakeNavidrome(map[string]string{"max": "secret", "buddymusic-scan": "scanpw"}))
	t.Cleanup(nav.Close)
	cfg.NavidromeURL = nav.URL
	cfg.ScanUser = "buddymusic-scan"
	cfg.ScanPassword = "scanpw"
	store, err := OpenStore(filepath.Join(cfg.DataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewApp(cfg, store)
}

func fakeNavidrome(users map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pw, ok := users[q.Get("u")]
		sum := md5.Sum([]byte(pw + q.Get("s")))
		w.Header().Set("Content-Type", "application/json")
		if !ok || hex.EncodeToString(sum[:]) != q.Get("t") {
			w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":40,"message":"Wrong username or password"}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/startScan.view") && q.Get("u") != "buddymusic-scan" {
			w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":50,"message":"not authorized"}}}`))
			return
		}
		w.Write([]byte(`{"subsonic-response":{"status":"ok","scanStatus":{"scanning":true}}}`))
	})
}

func login(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"Max","password":"secret"}`))
	rec := do(t, a.Routes(), req)
	if rec.Code != 200 {
		t.Fatalf("login status %d: %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func authed(t *testing.T, a *App, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req.AddCookie(login(t, a))
	return do(t, a.Routes(), req)
}

type formPart struct {
	field, filename string
	data            []byte
}

func multipartBody(t *testing.T, parts ...formPart) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w interface{ Write([]byte) (int, error) }
		var err error
		if p.filename != "" {
			w, err = mw.CreateFormFile(p.field, p.filename)
		} else {
			w, err = mw.CreateFormField(p.field)
		}
		if err != nil {
			t.Fatal(err)
		}
		w.Write(p.data)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	a := newTestApp(t)
	rec := do(t, a.Routes(), httptest.NewRequest("GET", "/api/health", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestUntrustedNetworkRejected(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.RemoteAddr = "203.0.113.5:4444"
	rec := httptest.NewRecorder()
	a.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSniffAudio(t *testing.T) {
	cases := []struct {
		head []byte
		name string
		want string
	}{
		{[]byte("fLaC\x00\x00"), "a.flac", ".flac"},
		{[]byte("ID3\x04\x00"), "a.mp3", ".mp3"},
		{[]byte{0xFF, 0xFB, 0x90}, "a.mp3", ".mp3"},
		{[]byte("\x00\x00\x00\x20ftypM4A \x00"), "a.m4a", ".m4a"},
		{[]byte("OggS\x00\x02........OpusHead"), "a.ogg", ".opus"},
		{[]byte("OggS\x00\x02........\x01vorbis"), "a.ogg", ".ogg"},
		{[]byte("hello world"), "a.mp3", ""},
	}
	for _, c := range cases {
		if got := sniffAudio(c.head, c.name); got != c.want {
			t.Errorf("%q: got %q want %q", c.head, got, c.want)
		}
	}
}

func TestSafeSegment(t *testing.T) {
	cases := map[string]string{
		"AC/DC":            "AC_DC",
		"..":               "_",
		"":                 "_",
		"  Hello   World ": "Hello World",
		"a\x00b":           "ab",
		"What?":            "What_",
		"../../etc":        "_.._etc",
		".hidden":          "hidden",
	}
	for in, want := range cases {
		if got := safeSegment(in); got != want {
			t.Errorf("safeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUploadRejectsNonAudio(t *testing.T) {
	a := newTestApp(t)
	body, ct := multipartBody(t, formPart{"file", "fake.mp3", []byte("this is not audio at all")})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := authed(t, a, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.IncomingDir(), "max"))
	if len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestUploadTooLarge(t *testing.T) {
	a := newTestApp(t)
	data := append([]byte("fLaC"), bytes.Repeat([]byte{0}, 2<<20)...)
	body, ct := multipartBody(t, formPart{"file", "big.flac", data})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := authed(t, a, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestUploadStoresFile(t *testing.T) {
	a := newTestApp(t)
	body, ct := multipartBody(t, formPart{"file", "song.flac", []byte("fLaC\x00\x00\x00\x22rest")})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	cookie := login(t, a)
	req.AddCookie(cookie)
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var u Upload
	json.Unmarshal(rec.Body.Bytes(), &u)
	if _, err := os.Stat(filepath.Join(a.cfg.IncomingDir(), "max", u.ID+".flac")); err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest("GET", "/api/uploads/"+u.ID, nil)
	get.AddCookie(cookie)
	rec = do(t, a.Routes(), get)
	if rec.Code != 200 {
		t.Fatalf("get status %d", rec.Code)
	}
}
