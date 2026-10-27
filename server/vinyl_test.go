package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func vinylApp(t *testing.T) *App {
	t.Helper()
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/Old/Disc/CD1/01 - One.mp3", "s2": "max/Old/Disc/CD2/02 - Two.mp3"}, map[string][]string{"al1": {"s1", "s2"}}))
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc/CD1/01 - One.mp3"))
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "max/Old/Disc/CD2/02 - Two.mp3"))
	return a
}

func meVinyl(t *testing.T, a *App) []string {
	t.Helper()
	rec := authed(t, a, httptest.NewRequest("GET", "/api/me", nil))
	var me struct {
		VinylAlbums []string `json:"vinylAlbums"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.VinylAlbums == nil {
		t.Fatalf("me: %v %s", err, rec.Body)
	}
	return me.VinylAlbums
}

func albumVinyl(t *testing.T, a *App) bool {
	t.Helper()
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil))
	if rec.Code != 200 {
		t.Fatalf("album: %d %s", rec.Code, rec.Body)
	}
	return strings.Contains(rec.Body.String(), `"vinyl":true`)
}

func TestVinylToggleAdminOnly(t *testing.T) {
	a := vinylApp(t)
	if rec := editReq(t, a, login(t, a), "al1", `{"vinyl":true}`); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin vinyl: %d %s", rec.Code, rec.Body)
	}
	if got := meVinyl(t, a); len(got) != 0 || albumVinyl(t, a) {
		t.Fatalf("non-admin vinyl stored: %v", got)
	}
}

func TestVinylToggle(t *testing.T) {
	a := vinylApp(t)
	boss := loginAs(t, a, "boss", "bosspw")
	if rec := editReq(t, a, boss, "al1", `{"vinyl":true}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scan":"skipped"`) {
		t.Fatalf("vinyl on: %d %s", rec.Code, rec.Body)
	}
	if got := meVinyl(t, a); len(got) != 1 || got[0] != "al1" {
		t.Fatalf("vinylAlbums: %v", got)
	}
	if !albumVinyl(t, a) {
		t.Fatal("album response lacks vinyl")
	}
	if rec := editReq(t, a, boss, "al1", `{"vinyl":false}`); rec.Code != 200 {
		t.Fatalf("vinyl off: %d %s", rec.Code, rec.Body)
	}
	if got := meVinyl(t, a); len(got) != 0 || albumVinyl(t, a) {
		t.Fatalf("vinyl still set: %v", got)
	}
}

func TestVinylFolderBeforeScan(t *testing.T) {
	a := vinylApp(t)
	if err := a.SetVinylFolder("max/Old/Disc", true); err != nil {
		t.Fatal(err)
	}
	if got := meVinyl(t, a); len(got) != 0 {
		t.Fatalf("unknown album id listed: %v", got)
	}
	if !albumVinyl(t, a) {
		t.Fatal("folder flag not applied to album")
	}
	if got := meVinyl(t, a); len(got) != 1 || got[0] != "al1" {
		t.Fatalf("album id not learned: %v", got)
	}
}

func TestUploadVinylFlag(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	one := makeAudio(t, "a.flac", true, "title", "Intro", "artist", "Test Band", "album", "Wax", "track", "1")
	rec, _ := postUpload(t, a, cookie, formPart{"vinyl", "", []byte("1")}, formPart{"file", "a.flac", one})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	if got := a.store.UnresolvedVinyl(); len(got) != 1 || got[0] != "max/Test Band/Wax" {
		t.Fatalf("vinyl folder: %v", got)
	}
	if name := a.store.FolderAlbumName("max/Test Band/Wax"); name != "Wax" {
		t.Fatalf("album name: %q", name)
	}
	plain := makeAudio(t, "b.flac", true, "title", "Intro", "artist", "Test Band", "album", "Digital", "track", "1")
	if rec, _ := postUpload(t, a, cookie, formPart{"vinyl", "", []byte("0")}, formPart{"file", "b.flac", plain}); rec.Code != http.StatusCreated {
		t.Fatalf("plain upload: %d %s", rec.Code, rec.Body)
	}
	if got := a.store.UnresolvedVinyl(); len(got) != 1 {
		t.Fatalf("unticked upload flagged: %v", got)
	}
}

func TestVinylFolderResolvedForMe(t *testing.T) {
	searchable := false
	searches := 0
	a := newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/search3.view") {
			return false
		}
		searches++
		if q := r.URL.Query().Get("query"); q != "Wax" {
			t.Errorf("search query %q", q)
		}
		songs := []map[string]string{{"id": "x1", "path": "boi/Wax/01 - Other.flac", "albumId": "other"}}
		if searchable {
			songs = append(songs, map[string]string{"id": "s1", "path": "max/Test Band/Wax/01 - Intro.flac", "albumId": "alw"})
		}
		b, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{"status": "ok", "searchResult3": map[string]any{"song": songs}}})
		w.Write(b)
		return true
	})
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "max/Test Band/Wax/01 - Intro.flac"))
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "boi/Wax/01 - Other.flac"))
	saveLibUpload(t, a, "u1", "max", "in-library", "max/Test Band/Wax/01 - Intro.flac", 1)
	if err := a.SetVinylFolder("max/Test Band/Wax", true); err != nil {
		t.Fatal(err)
	}
	if got := meVinyl(t, a); len(got) != 0 {
		t.Fatalf("not yet scanned: %v", got)
	}
	if got := meVinyl(t, a); len(got) != 0 || searches != 1 {
		t.Fatalf("retry not throttled: %v %d", got, searches)
	}
	searchable = true
	a.vinylTry = vinylThrottle{}
	if got := meVinyl(t, a); len(got) != 1 || got[0] != "alw" {
		t.Fatalf("vinylAlbums after scan: %v", got)
	}
	if got := meVinyl(t, a); len(got) != 1 || searches != 2 {
		t.Fatalf("resolved folder searched again: %v %d", got, searches)
	}
}
