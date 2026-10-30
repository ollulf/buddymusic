package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeEncoder(calls *int) encodeFunc {
	return func(ctx context.Context, src, dst string, t transcodeTier) error {
		*calls++
		return os.WriteFile(dst, []byte("ENC-"+t.Format+"-0123456789"), 0o640)
	}
}

func TestTranscodeSweepEncodesAndCleans(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	song := filepath.Join(lib, "max", "Artist", "Album", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(song), "cover.jpg"), []byte("jpg"), 0o644)
	calls := 0
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib, fakeEncoder(&calls))
	if n := tc.Sweep(context.Background()); n != len(transcodeTiers) || calls != len(transcodeTiers) {
		t.Fatalf("made %d calls %d", n, calls)
	}
	tier, _ := findTier("opus", 128)
	p, ok := tc.Lookup(song, tier)
	if !ok || !strings.HasSuffix(p, ".opus") {
		t.Fatalf("lookup %q %v", p, ok)
	}
	if n := tc.Sweep(context.Background()); n != 0 || calls != len(transcodeTiers) {
		t.Fatalf("second sweep re-encoded: %d", n)
	}
	os.Remove(song)
	tc.Sweep(context.Background())
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("stale transcode kept: %v", err)
	}
}

func TestTranscodeFailureLeavesNoFile(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	os.MkdirAll(lib, 0o755)
	song := filepath.Join(lib, "a.mp3")
	os.WriteFile(song, []byte("x"), 0o644)
	calls := 0
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib, func(ctx context.Context, src, dst string, t transcodeTier) error {
		calls++
		return os.ErrInvalid
	})
	tc.Sweep(context.Background())
	tc.Sweep(context.Background())
	if calls != len(transcodeTiers) {
		t.Fatalf("failed files retried: %d", calls)
	}
	if _, ok := tc.Lookup(song, transcodeTiers[0]); ok {
		t.Fatal("lookup found a failed transcode")
	}
}

func TestStreamServesPretranscoded(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 One.flac"}, nil))
	song := filepath.Join(a.cfg.LibraryDir(), "max", "A", "B", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	calls := 0
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), fakeEncoder(&calls))

	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "0123456789opus128") {
		t.Fatalf("before sweep should proxy live: %d %s", rec.Code, rec.Body)
	}
	a.transcodes.Sweep(context.Background())

	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128", nil))
	if rec.Code != 200 || rec.Body.String() != "ENC-opus-0123456789" || rec.Header().Get("Content-Type") != "audio/ogg" || rec.Header().Get("Content-Length") != "19" {
		t.Fatalf("pretranscoded: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=192", nil)
	req.Header.Set("Range", "bytes=4-6")
	rec = authed(t, a, req)
	if rec.Code != 206 || rec.Body.String() != "mp3" {
		t.Fatalf("range: %d %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=80", nil))
	if !strings.HasPrefix(rec.Body.String(), "0123456789opus80") {
		t.Fatalf("other bitrate should proxy live: %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128&timeOffset=30", nil))
	if !strings.HasPrefix(rec.Body.String(), "0123456789opus128") {
		t.Fatalf("timeOffset should proxy live: %s", rec.Body)
	}
	if d, err := a.store.Dashboard(time.Now(), 1); err != nil || d.Totals.Bytes <= 0 {
		t.Fatalf("stream bytes not recorded: %v", err)
	}
}
