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

func TestStreamStoreIgnoresStaleIfRange(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 One.flac"}, nil))
	song := filepath.Join(a.cfg.LibraryDir(), "max", "A", "B", "01 One.flac")
	os.MkdirAll(filepath.Dir(song), 0o755)
	os.WriteFile(song, []byte("flac"), 0o644)
	calls := 0
	a.transcodes = NewTranscodes(filepath.Join(a.cfg.DataDir, "transcodes"), a.cfg.LibraryDir(), fakeEncoder(&calls))
	a.transcodes.Sweep(context.Background())
	req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=192", nil)
	req.Header.Set("Range", "bytes=4-6")
	req.Header.Set("If-Range", "Mon, 02 Jan 2006 15:04:05 GMT")
	rec := authed(t, a, req)
	if rec.Code != 206 || rec.Body.String() != "mp3" {
		t.Fatalf("stale If-Range must still give the asked range, not the file start: %d %q", rec.Code, rec.Body)
	}
}

func TestStreamRefusesRangeAnsweredFromFileStart(t *testing.T) {
	cases := []struct {
		name   string
		cr     string
		body   []byte
		status int
	}{
		{"206 mp3 body from byte 0", "bytes 4000-9999/10000", append([]byte("ID3\x04\x00\x00"), bytes.Repeat([]byte{0xff}, 64)...), http.StatusRequestedRangeNotSatisfiable},
		{"206 ogg first page", "bytes 4000-9999/10000", append([]byte("OggS\x00\x02"), bytes.Repeat([]byte{1}, 64)...), http.StatusRequestedRangeNotSatisfiable},
		{"206 wrong Content-Range start", "bytes 0-9999/10000", bytes.Repeat([]byte{0xff}, 64), http.StatusRequestedRangeNotSatisfiable},
		{"206 correct middle", "bytes 4000-9999/10000", append([]byte("OggS\x00\x00"), bytes.Repeat([]byte{1}, 64)...), http.StatusPartialContent},
		{"206 short correct middle", "bytes 4000-4002/4003", []byte{0xff, 0xfb, 0x90}, http.StatusPartialContent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotIfRange string
			a := newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
				if !strings.HasSuffix(r.URL.Path, "/stream.view") {
					return false
				}
				gotIfRange = r.Header.Get("If-Range")
				w.Header().Set("Content-Type", "audio/mpeg")
				w.Header().Set("Content-Range", c.cr)
				w.WriteHeader(http.StatusPartialContent)
				w.Write(c.body)
				return true
			})
			req := httptest.NewRequest("GET", "/api/music/stream/s1?format=mp3&maxBitRate=128", nil)
			req.Header.Set("Range", "bytes=4000-")
			req.Header.Set("If-Range", "Mon, 02 Jan 2006 15:04:05 GMT")
			rec := authed(t, a, req)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d", rec.Code, c.status)
			}
			if c.status == http.StatusPartialContent && !bytes.Equal(rec.Body.Bytes(), c.body) {
				t.Fatalf("body changed: %q", rec.Body.Bytes())
			}
			if gotIfRange != "" {
				t.Fatalf("If-Range forwarded to navidrome: %q", gotIfRange)
			}
		})
	}
}
