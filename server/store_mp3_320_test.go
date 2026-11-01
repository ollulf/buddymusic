package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamMP3320FromStore(t *testing.T) {
	if _, ok := findTier("mp3", 320); !ok {
		t.Fatal("mp3 320 is not a store tier")
	}
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 One.flac"}, nil))
	song := filepath.Join(a.cfg.LibraryDir(), "max", "A", "B", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	enc := &countingEncoder{}
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), enc.encode)
	req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=320", nil)
	req.Header.Set("Range", "bytes=0-1")
	rec := authed(t, a, req)
	if rec.Code != 206 || rec.Body.String() != "EN" || rec.Header().Get("Content-Range") != "bytes 0-1/21" || rec.Header().Get("X-Pretranscoded") != "1" {
		t.Fatalf("probe: %d %v %q", rec.Code, rec.Header(), rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=320", nil))
	if rec.Code != 200 || rec.Body.String() != "ENC-mp3320-0123456789" {
		t.Fatalf("full: %d %q", rec.Code, rec.Body)
	}
}

func TestStreamProxyProbeAnsweredWithWholeStream(t *testing.T) {
	stream := bytes.Repeat([]byte{0xff, 0xfb}, 5000)
	cases := []struct {
		name   string
		length bool
		total  string
	}{
		{"no length", false, "*"},
		{"with length", true, "10000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
				if !strings.HasSuffix(r.URL.Path, "/stream.view") {
					return false
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				if c.length {
					w.Header().Set("Content-Length", "10000")
				}
				w.WriteHeader(http.StatusOK)
				w.Write(stream)
				return true
			})
			req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=256", nil)
			req.Header.Set("Range", "bytes=0-1")
			rec := authed(t, a, req)
			if rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), stream[:2]) || rec.Header().Get("Content-Range") != "bytes 0-1/"+c.total || rec.Header().Get("Content-Length") != "2" {
				t.Fatalf("probe: %d %v %d bytes", rec.Code, rec.Header(), rec.Body.Len())
			}
			rec = authed(t, a, httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=256", nil))
			if rec.Code != 200 || rec.Body.Len() != len(stream) {
				t.Fatalf("plain request: %d %d bytes", rec.Code, rec.Body.Len())
			}
		})
	}
}

func TestStreamProxyRawProbeUntouched(t *testing.T) {
	stream := bytes.Repeat([]byte{1}, 100)
	a := newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/stream.view") {
			return false
		}
		w.Header().Set("Content-Type", "audio/flac")
		w.WriteHeader(http.StatusOK)
		w.Write(stream)
		return true
	})
	req := httptest.NewRequest("GET", "/api/music/stream/s1", nil)
	req.Header.Set("Range", "bytes=0-1")
	rec := authed(t, a, req)
	if rec.Code != 200 || rec.Body.Len() != len(stream) {
		t.Fatalf("raw: %d %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestOnDemandEncodesAreCapped(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	var songs []string
	for _, n := range []string{"01 One.flac", "02 Two.flac", "03 Three.flac", "04 Four.flac", "05 Five.flac"} {
		p := filepath.Join(lib, "max", "A", "B", n)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(n), 0o644)
		songs = append(songs, p)
	}
	var running, peak int32
	enc := func(ctx context.Context, src, dst string, tier transcodeTier) error {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return os.WriteFile(dst, []byte("x"), 0o640)
	}
	tc := NewTranscodes(filepath.Join(root, "transcodes"), lib, enc)
	var wg sync.WaitGroup
	for _, s := range songs {
		for _, tier := range transcodeTiers {
			wg.Add(1)
			go func(s string, tier transcodeTier) {
				defer wg.Done()
				if _, err := tc.Ensure(context.Background(), s, tier); err != nil {
					t.Errorf("ensure: %v", err)
				}
			}(s, tier)
		}
	}
	wg.Wait()
	if peak > urgentEncodes {
		t.Fatalf("%d encodes ran at once, cap %d", peak, urgentEncodes)
	}
}
