package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func uploaderTestApp(t *testing.T, albums map[string][]string) *App {
	t.Helper()
	return newTestAppWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/getAlbum.view") {
			return false
		}
		paths, ok := albums[r.URL.Query().Get("id")]
		if !ok {
			return false
		}
		var songs []map[string]string
		for i, p := range paths {
			songs = append(songs, map[string]string{"id": "s" + string(rune('a'+i)), "path": p})
		}
		al, _ := json.Marshal(map[string]any{"id": r.URL.Query().Get("id"), "name": "X", "song": songs})
		w.Write([]byte(`{"subsonic-response":{"status":"ok","album":` + string(al) + `}}`))
		return true
	})
}

func libFile(t *testing.T, a *App, rel string) {
	t.Helper()
	p := filepath.Join(a.cfg.LibraryDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func saveLibUpload(t *testing.T, a *App, id, user, status, rel string, at int64) {
	t.Helper()
	if err := a.store.SaveUpload(&Upload{ID: id, User: user, Filename: filepath.Base(rel), Status: status, LibraryPath: filepath.FromSlash(rel), CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func albumUploaders(t *testing.T, a *App, id string) []string {
	t.Helper()
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/album/"+id, nil))
	if rec.Code != 200 {
		t.Fatalf("album %s: %d %s", id, rec.Code, rec.Body)
	}
	var out struct {
		Name       string   `json:"name"`
		Song       []any    `json:"song"`
		UploadedBy []string `json:"uploadedBy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "X" || len(out.Song) == 0 {
		t.Fatalf("album json lost fields: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "uploadedBy") != (len(out.UploadedBy) > 0) {
		t.Fatalf("empty uploadedBy sent: %s", rec.Body)
	}
	return out.UploadedBy
}

func TestAlbumUploadedBy(t *testing.T) {
	a := uploaderTestApp(t, map[string][]string{
		"up":     {"boi/Artist/Album/01 One.flac", "boi/Artist/Album/02 Two.flac"},
		"old":    {"admin/Old/Album/01 One.flac"},
		"multi":  {"Shared/Mix/01 A.flac", "Shared/Mix/02 B.flac"},
		"reject": {"admin/Rej/Album/01 One.flac"},
		"fake":   {"Gone/Album/01 - One.mp3"},
	})
	libFile(t, a, "boi/Artist/Album/01 One.flac")
	libFile(t, a, "boi/Artist/Album/02 Two.flac")
	libFile(t, a, "admin/Old/Album/01 One.flac")
	libFile(t, a, "Shared/Mix/01 A.flac")
	libFile(t, a, "Shared/Mix/02 B.flac")
	libFile(t, a, "admin/Rej/Album/01 One.flac")
	saveLibUpload(t, a, "u1", "boi", "in-library", "boi/Artist/Album/01 One.flac", 1)
	saveLibUpload(t, a, "u2", "boi", "in-library", "boi/Artist/Album/02 Two.flac", 2)
	saveLibUpload(t, a, "u3", "boi", "in-library", "boi/Artist/Album Deluxe/01 One.flac", 3)
	saveLibUpload(t, a, "u4", "max", "in-library", "Shared/Mix/02 B.flac", 4)
	saveLibUpload(t, a, "u5", "boi", "in-library", "Shared/Mix/01 A.flac", 5)
	saveLibUpload(t, a, "u6", "boi", "rejected", "admin/Rej/Album/01 One.flac", 6)
	saveLibUpload(t, a, "u7", "admin", "needs-tags", "admin/Old/Album/02 Two.flac", 7)
	saveLibUpload(t, a, "u8", "max", "in-library", "max/Gone/Album/01 One.mp3", 8)

	if got := albumUploaders(t, a, "up"); !reflect.DeepEqual(got, []string{"boi"}) {
		t.Fatalf("app upload: %v", got)
	}
	if got := albumUploaders(t, a, "old"); got != nil {
		t.Fatalf("pre-existing admin album: %v", got)
	}
	if got := albumUploaders(t, a, "multi"); !reflect.DeepEqual(got, []string{"max", "boi"}) {
		t.Fatalf("multiple uploaders: %v", got)
	}
	if got := albumUploaders(t, a, "reject"); got != nil {
		t.Fatalf("rejected upload counted: %v", got)
	}
	if got := albumUploaders(t, a, "fake"); !reflect.DeepEqual(got, []string{"max"}) {
		t.Fatalf("unresolved path fallback: %v", got)
	}
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/album/al1", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "uploadedBy") {
		t.Fatalf("album without paths: %d %s", rec.Code, rec.Body)
	}
}
