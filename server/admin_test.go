package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
