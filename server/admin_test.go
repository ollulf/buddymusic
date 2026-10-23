package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func songNav(paths map[string]string, album map[string][]string) func(w http.ResponseWriter, r *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		id := r.URL.Query().Get("id")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getSong.view"):
			p, ok := paths[id]
			if !ok {
				w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":70,"message":"not found"}}}`))
				return true
			}
			b, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{"status": "ok", "song": map[string]string{"id": id, "path": p}}})
			w.Write(b)
			return true
		case strings.HasSuffix(r.URL.Path, "/getAlbum.view") && album[id] != nil:
			var songs []map[string]string
			for _, sid := range album[id] {
				songs = append(songs, map[string]string{"id": sid, "path": paths[sid]})
			}
			b, _ := json.Marshal(map[string]any{"subsonic-response": map[string]any{"status": "ok", "album": map[string]any{"id": id, "song": songs}}})
			w.Write(b)
			return true
		}
		return false
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func adminReq(t *testing.T, a *App, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(loginAs(t, a, "boss", "bosspw"))
	return do(t, a.Routes(), req)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestMeShowsAdmin(t *testing.T) {
	a := newTestApp(t)
	rec := adminReq(t, a, "GET", "/api/me")
	if !strings.Contains(rec.Body.String(), `"admin":true`) {
		t.Fatalf("admin me: %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/me", nil))
	if !strings.Contains(rec.Body.String(), `"admin":false`) {
		t.Fatalf("user me: %s", rec.Body)
	}
}

func TestDeleteTrackNonAdmin(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 - One.mp3"}, nil))
	f := filepath.Join(a.cfg.LibraryDir(), "max/A/B/01 - One.mp3")
	writeFile(t, f)
	rec := authed(t, a, httptest.NewRequest("DELETE", "/api/music/track/s1", nil))
	if rec.Code != http.StatusForbidden || !exists(f) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDeleteTrackMovesToTrash(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "max/A/B/01 - One.mp3"}, nil))
	f := filepath.Join(a.cfg.LibraryDir(), "max/A/B/01 - One.mp3")
	other := filepath.Join(a.cfg.LibraryDir(), "max/A/B/02 - Two.mp3")
	writeFile(t, f)
	writeFile(t, other)
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "max/A/B/cover.jpg"))
	rec := adminReq(t, a, "DELETE", "/api/music/track/s1")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"max/A/B/01 - One.mp3"`) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if exists(f) || !exists(other) || !exists(filepath.Join(a.cfg.LibraryDir(), "max/A/B/cover.jpg")) {
		t.Fatal("wrong files moved")
	}
	matches, _ := filepath.Glob(filepath.Join(a.cfg.MusicRoot, "_trash", "*-s1", "max/A/B/01 - One.mp3"))
	if len(matches) != 1 {
		t.Fatalf("trash: %v", matches)
	}
}

func TestDeleteTrackFakePathFallback(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "A/B/01 - One.mp3"}, nil))
	f := filepath.Join(a.cfg.LibraryDir(), "max/A/B/01 - One.mp3")
	writeFile(t, f)
	rec := adminReq(t, a, "DELETE", "/api/music/track/s1")
	if rec.Code != 200 || exists(f) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDeleteRefusesTraversal(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{
		"up":   "../../etc/passwd",
		"abs":  "/etc/passwd",
		"up2":  "max/../../data/test.db",
		"link": "max/link.mp3",
	}, nil))
	outside := filepath.Join(a.cfg.MusicRoot, "data", "secret.mp3")
	writeFile(t, outside)
	os.MkdirAll(filepath.Join(a.cfg.LibraryDir(), "max"), 0o755)
	os.Symlink(outside, filepath.Join(a.cfg.LibraryDir(), "max/link.mp3"))
	for _, id := range []string{"up", "abs", "up2", "link"} {
		rec := adminReq(t, a, "DELETE", "/api/music/track/"+id)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d: %s", id, rec.Code, rec.Body)
		}
	}
	if !exists(outside) || exists(filepath.Join(a.cfg.MusicRoot, "_trash")) {
		t.Fatal("something was moved")
	}
}

func TestDeleteAlbumCleansUp(t *testing.T) {
	paths := map[string]string{"s1": "max/A/B/01 - One.mp3", "s2": "max/A/B/02 - Two.flac"}
	a := newTestAppWith(t, songNav(paths, map[string][]string{"alb": {"s1", "s2"}}))
	for _, p := range paths {
		writeFile(t, filepath.Join(a.cfg.LibraryDir(), p))
	}
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "max/A/B/cover.png"))
	rec := adminReq(t, a, "DELETE", "/api/music/album/alb")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Moved []string `json:"moved"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Moved) != 3 {
		t.Fatalf("moved %v", out.Moved)
	}
	if exists(filepath.Join(a.cfg.LibraryDir(), "max/A")) || !exists(filepath.Join(a.cfg.LibraryDir(), "max")) {
		t.Fatal("dirs not cleaned up correctly")
	}
	matches, _ := filepath.Glob(filepath.Join(a.cfg.MusicRoot, "_trash", "*-alb", "max/A/B/cover.png"))
	if len(matches) != 1 {
		t.Fatalf("cover not in trash: %v", matches)
	}
}

func TestDeleteNeedsSameOrigin(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("DELETE", "/api/music/track/s1", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(loginAs(t, a, "boss", "bosspw"))
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cross-origin") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDeleteAlbumArtistTagPath(t *testing.T) {
	paths := map[string]string{"s1": "Russel Shaw, Danny Elfman/Fable/02 - Oakvale.flac"}
	a := newTestAppWith(t, songNav(paths, map[string][]string{"alb": {"s1"}}))
	f := filepath.Join(a.cfg.LibraryDir(), "admin/Russel Shaw/Fable/02 - Oakvale.flac")
	writeFile(t, f)
	writeFile(t, filepath.Join(a.cfg.LibraryDir(), "admin/Russel Shaw/Other/02 - Oakvale.flac"))
	rec := adminReq(t, a, "DELETE", "/api/music/album/alb")
	if rec.Code != 200 || exists(f) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	matches, _ := filepath.Glob(filepath.Join(a.cfg.MusicRoot, "_trash", "*-alb", "admin/Russel Shaw/Fable/02 - Oakvale.flac"))
	if len(matches) != 1 {
		t.Fatalf("trash: %v", matches)
	}
}

func TestDeleteAmbiguousAlbumFileRefused(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "Various/Fable/01 - Theme.flac"}, nil))
	f1 := filepath.Join(a.cfg.LibraryDir(), "admin/Russel Shaw/Fable/01 - Theme.flac")
	f2 := filepath.Join(a.cfg.LibraryDir(), "max/Danny Elfman/Fable/01 - Theme.flac")
	writeFile(t, f1)
	writeFile(t, f2)
	rec := adminReq(t, a, "DELETE", "/api/music/track/s1")
	if rec.Code != http.StatusConflict || !exists(f1) || !exists(f2) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestDeleteAlbumFileSymlinkRefused(t *testing.T) {
	a := newTestAppWith(t, songNav(map[string]string{"s1": "X/Fable/01 - Theme.flac"}, nil))
	outside := filepath.Join(a.cfg.MusicRoot, "data", "secret.flac")
	writeFile(t, outside)
	link := filepath.Join(a.cfg.LibraryDir(), "admin/Y/Fable/01 - Theme.flac")
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink(outside, link)
	rec := adminReq(t, a, "DELETE", "/api/music/track/s1")
	if rec.Code != http.StatusForbidden || !exists(outside) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func crossDevice(t *testing.T) {
	orig := renameFile
	renameFile = func(src, dst string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameFile = orig })
}

func TestDeleteCrossDeviceFallback(t *testing.T) {
	crossDevice(t)
	paths := map[string]string{"s1": "max/A/B/01 - One.mp3"}
	a := newTestAppWith(t, songNav(paths, map[string][]string{"alb": {"s1"}}))
	f := filepath.Join(a.cfg.LibraryDir(), "max/A/B/01 - One.mp3")
	writeFile(t, f)
	cover := filepath.Join(a.cfg.LibraryDir(), "max/A/B/cover.jpg")
	writeFile(t, cover)
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(f, mtime, mtime)
	os.Chmod(f, 0o640)
	rec := adminReq(t, a, "DELETE", "/api/music/album/alb")
	if rec.Code != 200 || exists(f) || exists(cover) || exists(filepath.Join(a.cfg.LibraryDir(), "max/A")) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	matches, _ := filepath.Glob(filepath.Join(a.cfg.MusicRoot, "_trash", "*-alb", "max/A/B/01 - One.mp3"))
	if len(matches) != 1 {
		t.Fatalf("trash: %v", matches)
	}
	st, err := os.Stat(matches[0])
	if err != nil || !st.ModTime().Equal(mtime) || st.Mode().Perm() != 0o640 {
		t.Fatalf("copy lost metadata: %v %v", st, err)
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "x" {
		t.Fatalf("content %q", b)
	}
	if c, _ := filepath.Glob(filepath.Join(a.cfg.MusicRoot, "_trash", "*-alb", "max/A/B/cover.jpg")); len(c) != 1 {
		t.Fatalf("cover: %v", c)
	}
}

func TestMoveFileCleansUpPartialCopy(t *testing.T) {
	crossDevice(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.Mkdir(src, 0o755)
	dst := filepath.Join(dir, "dst")
	if err := moveFile(src, dst); err == nil {
		t.Fatal("expected error")
	}
	if exists(dst) || !exists(src) {
		t.Fatal("partial copy left behind")
	}
}
