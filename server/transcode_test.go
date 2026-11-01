package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	a.transcodes.Sweep(context.Background())

	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128", nil))
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
	if rec.Body.String() != "ENC-opus-0123456789" {
		t.Fatalf("opus 80 should come from the store: %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=320", nil))
	if !strings.HasPrefix(rec.Body.String(), "0123456789mp3320") {
		t.Fatalf("bitrate without a store tier should proxy live: %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128&timeOffset=30", nil))
	if !strings.HasPrefix(rec.Body.String(), "0123456789opus128") {
		t.Fatalf("timeOffset should proxy live: %s", rec.Body)
	}
	if d, err := a.store.Dashboard(time.Now(), 1); err != nil || d.Totals.Bytes <= 0 {
		t.Fatalf("stream bytes not recorded: %v", err)
	}
}

type countingEncoder struct {
	mu    sync.Mutex
	calls int
	fail  bool
	delay time.Duration
}

func (c *countingEncoder) encode(ctx context.Context, src, dst string, t transcodeTier) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	time.Sleep(c.delay)
	if c.fail {
		return os.ErrInvalid
	}
	return os.WriteFile(dst, []byte("ENC-"+t.Format+strconv.Itoa(t.Kbps)+"-0123456789"), 0o640)
}

func (c *countingEncoder) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestStreamEncodesOnDemandOnce(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 One.flac"}, nil))
	song := filepath.Join(a.cfg.LibraryDir(), "max", "A", "B", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	enc := &countingEncoder{delay: 100 * time.Millisecond}
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), enc.encode)

	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 4)
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=128", nil)
			if i == 3 {
				req.Header.Set("Range", "bytes=0-1")
			}
			recs[i] = authed(t, a, req)
		}(i)
	}
	wg.Wait()
	for i, rec := range recs[:3] {
		if rec.Code != 200 || rec.Body.String() != "ENC-mp3128-0123456789" || rec.Header().Get("Content-Length") != "21" || rec.Header().Get("X-Pretranscoded") != "1" {
			t.Fatalf("request %d: %d %v %s", i, rec.Code, rec.Header(), rec.Body)
		}
	}
	if recs[3].Code != 206 || recs[3].Body.String() != "EN" || recs[3].Header().Get("Content-Range") != "bytes 0-1/21" {
		t.Fatalf("probe range: %d %v %s", recs[3].Code, recs[3].Header(), recs[3].Body)
	}
	if enc.count() != 1 {
		t.Fatalf("encoded %d times", enc.count())
	}
	if n := a.transcodes.Sweep(context.Background()); n != len(transcodeTiers)-1 || enc.count() != len(transcodeTiers) {
		t.Fatalf("sweep made %d, calls %d", n, enc.count())
	}
}

func TestStreamFallsBackToProxyWhenEncodeFails(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 One.flac"}, nil))
	song := filepath.Join(a.cfg.LibraryDir(), "max", "A", "B", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	enc := &countingEncoder{fail: true}
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), enc.encode)
	for i := 0; i < 2; i++ {
		rec := authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=192", nil))
		if !strings.HasPrefix(rec.Body.String(), "0123456789mp3192") {
			t.Fatalf("failed encode should proxy live: %s", rec.Body)
		}
	}
	if enc.count() != 1 {
		t.Fatalf("failed encode retried: %d", enc.count())
	}
}

func TestStreamFindsSweptSongBySizeWhenPathIsFake(t *testing.T) {
	var a *App
	nav := func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/getSong.view") {
			return false
		}
		b, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{"status": "ok",
			"song": map[string]any{"id": "s1", "path": "Tag Artist/Tag Album/01-03 - What?.flac", "size": 11}}})
		w.Write(b)
		return true
	}
	a = newTestAppWith(t, nav)
	song := filepath.Join(a.cfg.LibraryDir(), "max", "Artist", "Album", "03 - What_.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac-sized!"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(song), "04 - Other.flac"), []byte("other"), 0o644)
	enc := &countingEncoder{}
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), enc.encode)
	a.transcodes.Sweep(context.Background())
	calls := enc.count()
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=192", nil))
	if rec.Code != 200 || rec.Body.String() != "ENC-mp3192-0123456789" || rec.Header().Get("X-Pretranscoded") != "1" {
		t.Fatalf("store by size: %d %s", rec.Code, rec.Body)
	}
	if enc.count() != calls {
		t.Fatal("served song was encoded again")
	}
}

func TestTranscodeLookupThroughSymlinkedLibrary(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "disk", "library")
	song := filepath.Join(real, "max", "A", "B", "01.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	lib := filepath.Join(root, "library")
	if err := os.Symlink(real, lib); err != nil {
		t.Skip(err)
	}
	enc := &countingEncoder{}
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib+string(filepath.Separator), enc.encode)
	tc.Sweep(context.Background())
	resolved, _ := filepath.EvalSymlinks(song)
	tier, _ := findTier("mp3", 192)
	p1, ok1 := tc.Lookup(filepath.Join(lib, "max", "A", "B", "01.flac"), tier)
	p2, ok2 := tc.Lookup(resolved, tier)
	if !ok1 || !ok2 || p1 != p2 {
		t.Fatalf("lookup %q %v / %q %v", p1, ok1, p2, ok2)
	}
}
