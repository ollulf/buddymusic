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
