package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func navSongAt(path string, size int64) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/getSong.view") {
			return false
		}
		b, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{"status": "ok",
			"song": map[string]any{"id": "s1", "path": path, "size": size}}})
		w.Write(b)
		return true
	}
}

func TestStreamFindsSongWhileFirstSweepStillEncodes(t *testing.T) {
	content := []byte("flac-lonely-song")
	a := newTestAppWith(t, navSongAt("Ginuwine/The Bachelor/01-07 - Only When Ur Lonely.flac", int64(len(content))))
	lib := a.cfg.LibraryDir()
	os.MkdirAll(filepath.Join(lib, "anna", "Aaa", "First"), 0o755)
	os.WriteFile(filepath.Join(lib, "anna", "Aaa", "First", "01 - Busy.flac"), []byte("busy"), 0o644)
	song := filepath.Join(lib, "max", "Ginuwine", "The Bachelor", "07 - Only When Ur Lonely.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, content, 0o644)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	slow := func(ctx context.Context, src, dst string, tier transcodeTier) error {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return os.WriteFile(dst, []byte("slow"), 0o640)
	}
	fast := &countingEncoder{}
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), lib, slow)
	a.transcodes.urgent = fast.encode
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)
	go a.transcodes.Run(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background sweep did not start")
	}

	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=192", nil))
	if rec.Code != 200 || rec.Body.String() != "ENC-mp3192-0123456789" || rec.Header().Get("X-Pretranscoded") != "1" {
		t.Fatalf("on-demand during sweep: %d %s", rec.Code, rec.Body)
	}
	if fast.count() != 1 {
		t.Fatalf("urgent encodes %d", fast.count())
	}
}

func TestStoreFindPicksSameSizeByTitle(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	a := filepath.Join(lib, "max", "Ginuwine", "The Bachelor", "07 - Only When Ur Lonely.flac")
	b := filepath.Join(lib, "max", "Ginuwine", "The Bachelor", "08 - Something Else.flac")
	c := filepath.Join(lib, "anna", "Other", "Record", "02 Only When Ur Lonely.flac")
	for _, p := range []string{a, b, c} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("same-size"), 0o644)
	}
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib, fakeEncoder(new(int)))
	got, ok := tc.find(9, "Ginuwine/The Bachelor/01-07 - Only When Ur Lonely.flac")
	if !ok || got != a {
		t.Fatalf("find by size+title: %q %v", got, ok)
	}
	if got, ok := tc.find(9, "X/Y/01-08 Something Else.flac"); !ok || got != b {
		t.Fatalf("find by size+name: %q %v", got, ok)
	}
	if _, ok := tc.find(9, "X/Y/01-09 - Missing.flac"); ok {
		t.Fatal("ambiguous size must not match")
	}
	if got, ok := tc.find(12345, "Ginuwine/The Bachelor/1-07 Only When Ur Lonely.flac"); !ok || got != a {
		t.Fatalf("find by album+title when size changed: %q %v", got, ok)
	}
}

func TestTranscodeKickIndexesNewUpload(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	os.MkdirAll(filepath.Join(lib, "max", "A", "B"), 0o755)
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib, fakeEncoder(new(int)))
	tc.Index(context.Background())
	song := filepath.Join(lib, "max", "A", "B", "01 New.flac")
	os.WriteFile(song, []byte("new-upload"), 0o644)
	tc.Kick()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := tc.bySize(10); ok && p == song {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("kick did not index the new file")
}
