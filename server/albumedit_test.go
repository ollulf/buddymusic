package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func editReq(t *testing.T, a *App, cookie *http.Cookie, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PATCH", "/api/admin/album/"+id, strings.NewReader(body))
	req.AddCookie(cookie)
	return do(t, a.Routes(), req)
}

func editApp(t *testing.T) (*App, string, string) {
	t.Helper()
	needFFmpeg(t)
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/Old/Disc/01 - One.mp3", "s2": "max/Old/Disc/02 - Two.flac"}, map[string][]string{"al1": {"s1", "s2"}}))
	one := filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc/01 - One.mp3")
	two := filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc/02 - Two.flac")
	os.MkdirAll(filepath.Dir(one), 0o755)
	os.WriteFile(one, makeAudio(t, "a.mp3", true, "title", "One", "artist", "Old", "album", "Disc", "track", "1"), 0o644)
	os.WriteFile(two, makeAudio(t, "b.flac", true, "title", "Two", "artist", "Old", "album", "Disc", "track", "2"), 0o644)
	return a, one, two
}

func TestAlbumEditAdminOnly(t *testing.T) {
	a, one, _ := editApp(t)
	before, _ := os.ReadFile(one)
	rec := editReq(t, a, login(t, a), "al1", `{"album":"New"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin edit: %d %s", rec.Code, rec.Body)
	}
	after, _ := os.ReadFile(one)
	if string(before) != string(after) {
		t.Fatal("non-admin edit changed the file")
	}
}

func TestAlbumEditWritesTags(t *testing.T) {
	a, one, two := editApp(t)
	rec := editReq(t, a, loginAs(t, a, "boss", "bosspw"), "al1",
		`{"album":"New Disc","albumArtist":"Russel Shaw;Danny Elfman","year":"1999","genre":"Soundtrack","tracks":[{"id":"s2","title":"Zwei","artist":"Russel Shaw; Danny Elfman"}]}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"updated":2`) || !strings.Contains(rec.Body.String(), `"scan":"started"`) {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	t1, err := probe(context.Background(), one)
	if err != nil {
		t.Fatal(err)
	}
	if t1.Fields["album"] != "New Disc" || t1.Fields["albumartist"] != "Russel Shaw; Danny Elfman" || t1.Fields["year"] != "1999" ||
		t1.Fields["genre"] != "Soundtrack" || t1.Fields["title"] != "One" || t1.Fields["artist"] != "Old" || !t1.Cover {
		t.Fatalf("track 1 tags: %v cover %v", t1.Fields, t1.Cover)
	}
	t2, err := probe(context.Background(), two)
	if err != nil {
		t.Fatal(err)
	}
	if t2.Fields["title"] != "Zwei" || t2.Fields["artist"] != "Russel Shaw; Danny Elfman" || t2.Fields["album"] != "New Disc" || t2.Fields["track"] != "2" || !t2.Cover {
		t.Fatalf("track 2 tags: %v cover %v", t2.Fields, t2.Cover)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(one), ".*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestAlbumEditScanFails(t *testing.T) {
	a, one, _ := editApp(t)
	a.scanner = NewScanner(a.nav, "buddymusic-scan", "wrong", time.Millisecond)
	rec := editReq(t, a, loginAs(t, a, "boss", "bosspw"), "al1", `{"year":"2001"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scan":"failed"`) {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	if tags, _ := probe(context.Background(), one); tags == nil || tags.Fields["year"] != "2001" {
		t.Fatalf("year not written: %v", tags)
	}
}

func TestAlbumEditRejectsBadInput(t *testing.T) {
	a, _, _ := editApp(t)
	boss := loginAs(t, a, "boss", "bosspw")
	for _, body := range []string{`{"album":"  "}`, `{"year":"19x9"}`, `{"tracks":[{"id":"nope","title":"X"}]}`, `{"tracks":[{"id":"s1","title":""}]}`, `nope`} {
		if rec := editReq(t, a, boss, "al1", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}
