package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
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
	store, err := OpenStore(filepath.Join(cfg.DataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewApp(cfg, store)
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
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.IncomingDir(), "dev"))
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
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestUploadStoresFile(t *testing.T) {
	a := newTestApp(t)
	body, ct := multipartBody(t, formPart{"file", "song.flac", []byte("fLaC\x00\x00\x00\x22rest")})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var u Upload
	json.Unmarshal(rec.Body.Bytes(), &u)
	if _, err := os.Stat(filepath.Join(a.cfg.IncomingDir(), "dev", u.ID+".flac")); err != nil {
		t.Fatal(err)
	}
	rec = do(t, a.Routes(), httptest.NewRequest("GET", "/api/uploads/"+u.ID, nil))
	if rec.Code != 200 {
		t.Fatalf("get status %d", rec.Code)
	}
}
