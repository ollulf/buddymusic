package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func ownerDeleteApp(t *testing.T) (*App, map[string]string) {
	t.Helper()
	paths := map[string]string{
		"s1": "max/A/Mine/01 - One.mp3", "s2": "max/A/Mine/02 - Two.mp3",
		"s3": "boss/B/Theirs/01 - One.mp3",
		"s4": "Shared/Mix/01 - A.mp3", "s5": "Shared/Mix/02 - B.mp3",
		"s6": "old/C/Old/01 - One.mp3",
	}
	a := newTestAppWith(t, songNav(paths, map[string][]string{
		"mine": {"s1", "s2"}, "theirs": {"s3"}, "mixed": {"s4", "s5"}, "old": {"s6"},
	}))
	for _, p := range paths {
		writeFile(t, filepath.Join(a.cfg.LibraryDir(), p))
	}
	saveLibUpload(t, a, "u1", "max", "in-library", paths["s1"], 1)
	saveLibUpload(t, a, "u2", "max", "in-library", paths["s2"], 2)
	saveLibUpload(t, a, "u3", "boss", "in-library", paths["s3"], 3)
	saveLibUpload(t, a, "u4", "max", "in-library", paths["s4"], 4)
	saveLibUpload(t, a, "u5", "boss", "in-library", paths["s5"], 5)
	return a, paths
}

func TestDeleteAlbumByUploader(t *testing.T) {
	a, paths := ownerDeleteApp(t)
	rec := authed(t, a, httptest.NewRequest("DELETE", "/api/music/album/mine", nil))
	if rec.Code != 200 || exists(filepath.Join(a.cfg.LibraryDir(), paths["s1"])) {
		t.Fatalf("owner delete: %d %s", rec.Code, rec.Body)
	}
}

func TestDeleteAlbumNotOwner(t *testing.T) {
	a, paths := ownerDeleteApp(t)
	for _, id := range []string{"theirs", "mixed", "old"} {
		rec := authed(t, a, httptest.NewRequest("DELETE", "/api/music/album/"+id, nil))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "not-owner") {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	for _, p := range []string{"s3", "s4", "s5", "s6"} {
		if !exists(filepath.Join(a.cfg.LibraryDir(), paths[p])) {
			t.Fatalf("%s deleted", p)
		}
	}
	rec := adminReq(t, a, "DELETE", "/api/music/album/mixed")
	if rec.Code != 200 || exists(filepath.Join(a.cfg.LibraryDir(), paths["s4"])) {
		t.Fatalf("admin delete: %d %s", rec.Code, rec.Body)
	}
}

func TestDeleteTrackStaysAdminOnlyForUploader(t *testing.T) {
	a, paths := ownerDeleteApp(t)
	rec := authed(t, a, httptest.NewRequest("DELETE", "/api/music/track/s1", nil))
	if rec.Code != http.StatusForbidden || !exists(filepath.Join(a.cfg.LibraryDir(), paths["s1"])) {
		t.Fatalf("track: %d %s", rec.Code, rec.Body)
	}
}
