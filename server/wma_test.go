package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestSniffWMA(t *testing.T) {
	head := append(append([]byte{}, asfMagic...), 0, 0, 0, 0)
	if got := sniffAudio(head, "a.mp3"); got != ".wma" {
		t.Fatalf("got %q", got)
	}
	if got := sniffAudio(asfMagic[:8], "a.wma"); got != "" {
		t.Fatalf("partial guid: %q", got)
	}
}

func TestWMATarget(t *testing.T) {
	cases := map[string]string{"wmalossless": ".flac", "wmav1": ".mp3", "wmav2": ".mp3", "wmapro": ".mp3"}
	for codec, want := range cases {
		if got, _, err := wmaTarget(codec); err != nil || got != want {
			t.Errorf("%s: %q %v", codec, got, err)
		}
	}
	if _, _, err := wmaTarget("h264"); err != errNotAudio {
		t.Fatalf("video codec: %v", err)
	}
}

func TestUploadWMA(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	data := makeAudio(t, "a.wma", true, "title", "Old Song", "artist", "Win Band", "album", "Media", "track", "4", "date", "2003")
	if sniffAudio(data[:512], "a.wma") != ".wma" {
		t.Fatal("generated file not detected as ASF")
	}
	rec, body := postUpload(t, a, cookie, formPart{"file", "a.wma", data})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if body["libraryPath"] != "max/Win Band/Media/04 - Old Song.mp3" {
		t.Fatalf("path %v", body["libraryPath"])
	}
	dst := filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string))
	tags, err := probe(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Fields["title"] != "Old Song" || tags.Fields["year"] != "2003" || !tags.Cover {
		t.Fatalf("tags %v cover %v", tags.Fields, tags.Cover)
	}
	if !hasCoverFile(filepath.Dir(dst)) {
		t.Fatal("cover.jpg not written")
	}
	rec, body = postUpload(t, a, cookie, formPart{"file", "a.wma", data})
	if rec.Code != http.StatusConflict || body["error"] != "exists" {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.IncomingDir(), "max"))
	if len(entries) != 0 {
		t.Fatalf("leftover incoming files: %v", entries)
	}
}

func TestUploadBrokenWMA(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	data := append(append([]byte{}, asfMagic...), make([]byte, 600)...)
	rec, _ := postUpload(t, a, cookie, formPart{"file", "x.wma", data})
	if rec.Code == http.StatusCreated {
		t.Fatalf("broken wma accepted: %s", rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.IncomingDir(), "max"))
	if len(entries) != 0 {
		t.Fatalf("leftover incoming files: %v", entries)
	}
}
