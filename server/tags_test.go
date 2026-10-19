package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseProbe(t *testing.T) {
	cases := []struct {
		name  string
		json  string
		want  map[string]string
		cover bool
		err   error
	}{
		{"flac upper keys", `{"format":{"format_name":"flac","tags":{"TITLE":"A","ARTIST":"B","ALBUM":"C","TRACKNUMBER":"3/12","ALBUMARTIST":"D","DATE":"2020"}},"streams":[{"codec_type":"audio"}]}`,
			map[string]string{"title": "A", "artist": "B", "album": "C", "track": "3/12", "year": "2020"}, false, nil},
		{"ogg stream tags", `{"format":{"format_name":"ogg"},"streams":[{"codec_type":"audio","tags":{"title":"T","artist":"A","album":"B","track":"1"}}]}`,
			map[string]string{"title": "T", "artist": "A", "album": "B", "track": "1"}, false, nil},
		{"mp3 cover", `{"format":{"format_name":"mp3","tags":{"title":"  x\ty  "}},"streams":[{"codec_type":"audio"},{"codec_type":"video","disposition":{"attached_pic":1}}]}`,
			map[string]string{"title": "x y"}, true, nil},
		{"video only", `{"format":{"format_name":"mov,mp4,m4a"},"streams":[{"codec_type":"video"}]}`, nil, false, errNotAudio},
		{"wav", `{"format":{"format_name":"wav"},"streams":[{"codec_type":"audio"}]}`, nil, false, errNotAudio},
	}
	for _, c := range cases {
		got, err := parseProbe([]byte(c.json))
		if err != c.err {
			t.Errorf("%s: err %v", c.name, err)
			continue
		}
		if err != nil {
			continue
		}
		if !reflect.DeepEqual(got.Fields, c.want) || got.Cover != c.cover {
			t.Errorf("%s: got %v cover=%v", c.name, got.Fields, got.Cover)
		}
	}
}

func TestMissingTags(t *testing.T) {
	cases := []struct {
		fields map[string]string
		cover  bool
		want   []string
	}{
		{map[string]string{"title": "a", "artist": "b", "album": "c", "track": "1"}, true, []string{}},
		{map[string]string{"title": "a", "artist": "b", "album": "c", "track": "0"}, true, []string{"track"}},
		{map[string]string{"title": "a", "artist": "b", "album": "c", "track": "x/2"}, true, []string{"track"}},
		{map[string]string{"title": "a"}, false, []string{"artist", "album", "track", "cover"}},
	}
	for _, c := range cases {
		if got := missingTags(c.fields, c.cover); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v want %v", c.fields, got, c.want)
		}
	}
}

func TestParseTypedAndMerge(t *testing.T) {
	typed, err := parseTyped([]byte(`{"album":" New  Album ","track":3,"title":"","evil":"x","artist":"A\u0000B"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"album": "New Album", "track": "3", "artist": "A B"}
	if !reflect.DeepEqual(typed, want) {
		t.Fatalf("typed %v", typed)
	}
	merged := mergeTags(&Tags{Fields: map[string]string{"title": "T", "album": "Old"}}, typed)
	if merged["album"] != "New Album" || merged["title"] != "T" {
		t.Fatalf("merged %v", merged)
	}
}

func TestTrackFileName(t *testing.T) {
	if got := trackFileName(map[string]string{"track": "3/12", "title": "A/B"}, ".flac"); got != "03 - A_B.flac" {
		t.Fatal(got)
	}
	if got := trackFileName(map[string]string{"track": "1", "title": "X", "disc": "2/2"}, ".mp3"); got != "2-01 - X.mp3" {
		t.Fatal(got)
	}
}

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skip(b + " not installed")
		}
	}
}

func makeAudio(t *testing.T, name string, cover bool, meta ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, name)
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=d=1"}
	if cover {
		img := filepath.Join(dir, "c.jpg")
		if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x32:d=1", "-frames:v", "1", img).CombinedOutput(); err != nil {
			t.Fatalf("cover: %v %s", err, b)
		}
		args = append(args, "-i", img, "-map", "0", "-map", "1", "-c:v", "mjpeg", "-disposition:v", "attached_pic")
	}
	switch filepath.Ext(name) {
	case ".mp3":
		args = append(args, "-c:a", "libmp3lame")
	case ".opus":
		args = append(args, "-c:a", "libopus")
	case ".m4a":
		args = append(args, "-c:a", "aac")
	}
	for i := 0; i+1 < len(meta); i += 2 {
		args = append(args, "-metadata", meta[i]+"="+meta[i+1])
	}
	args = append(args, out)
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jpeg(t *testing.T) []byte {
	t.Helper()
	img := filepath.Join(t.TempDir(), "c.jpg")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=32x32:d=1", "-frames:v", "1", img).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	data, _ := os.ReadFile(img)
	return data
}

func postUpload(t *testing.T, a *App, cookie *http.Cookie, parts ...formPart) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	body, ct := multipartBody(t, parts...)
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	rec := do(t, a.Routes(), req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestUploadTagGate(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	full := makeAudio(t, "a.flac", true, "title", "Intro", "artist", "Test Band", "album", "Demo", "track", "1")
	partial := makeAudio(t, "b.mp3", false, "title", "Memo", "artist", "Test Band")

	rec, body := postUpload(t, a, cookie, formPart{"file", "b.mp3", partial})
	if rec.Code != http.StatusUnprocessableEntity || body["error"] != "missing-tags" {
		t.Fatalf("partial: %d %s", rec.Code, rec.Body)
	}
	if got := body["missing"]; !reflect.DeepEqual(got, []any{"album", "track", "cover"}) {
		t.Fatalf("missing %v", got)
	}

	rec, body = postUpload(t, a, cookie, formPart{"file", "a.flac", full})
	if rec.Code != http.StatusCreated {
		t.Fatalf("full: %d %s", rec.Code, rec.Body)
	}
	if body["libraryPath"] != "max/Test Band/Demo/01 - Intro.flac" {
		t.Fatalf("path %v", body["libraryPath"])
	}

	rec, body = postUpload(t, a, cookie, formPart{"tags", "", []byte(`{"album":"Demo","track":"2"}`)}, formPart{"file", "b.mp3", partial})
	if rec.Code != http.StatusCreated {
		t.Fatalf("cover from the album's first track: %d %s", rec.Code, rec.Body)
	}
	if body["libraryPath"] != "max/Test Band/Demo/02 - Memo.mp3" {
		t.Fatalf("path %v", body["libraryPath"])
	}
}

func TestUploadTypedTagsAndCover(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	partial := makeAudio(t, "b.mp3", false, "title", "Memo", "artist", "Test Band")
	rec, body := postUpload(t, a, cookie,
		formPart{"tags", "", []byte(`{"album":"Fresh","track":"2","album_artist":"Various"}`)},
		formPart{"cover", "c.jpg", jpeg(t)},
		formPart{"file", "b.mp3", partial})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	dst := filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string))
	if !strings.HasSuffix(dst, "max/Test Band/Fresh/02 - Memo.mp3") {
		t.Fatal(dst)
	}
	tags, err := probe(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Fields["album"] != "Fresh" || number(tags.Fields["track"]) != 2 {
		t.Fatalf("written tags %v", tags.Fields)
	}
	if !hasCoverFile(filepath.Dir(dst)) {
		t.Fatal("cover.jpg not written")
	}

	other := makeAudio(t, "c.mp3", false, "title", "Second", "artist", "Test Band", "album", "Fresh", "album_artist", "Various", "track", "3")
	rec, body = postUpload(t, a, cookie, formPart{"file", "c.mp3", other})
	if rec.Code != http.StatusCreated {
		t.Fatalf("cover from folder not accepted: %d %s", rec.Code, rec.Body)
	}
	if body["libraryPath"] != "max/Test Band/Fresh/03 - Second.mp3" {
		t.Fatalf("album artist tag changed folder: %v", body["libraryPath"])
	}
	rec, body = postUpload(t, a, cookie, formPart{"file", "c.mp3", other})
	if rec.Code != http.StatusConflict || body["error"] != "exists" {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(a.cfg.IncomingDir(), "max"))
	if len(entries) != 0 {
		t.Fatalf("incoming not cleaned: %v", entries)
	}
}

func png(t *testing.T) []byte {
	t.Helper()
	img := filepath.Join(t.TempDir(), "c.png")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=green:s=32x32:d=1", "-frames:v", "1", img).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	data, _ := os.ReadFile(img)
	return data
}

func TestPickedCoverReplacesAlbumCover(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	first := makeAudio(t, "a.mp3", true, "title", "One", "artist", "Band", "album", "Covers", "track", "1")
	rec, body := postUpload(t, a, cookie, formPart{"file", "a.mp3", first})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	dir := filepath.Dir(filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string)))
	extracted, err := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if err != nil {
		t.Fatal("embedded cover not extracted")
	}

	picked := jpeg(t)
	second := makeAudio(t, "b.mp3", false, "title", "Two", "artist", "Band", "album", "Covers", "track", "2")
	rec, _ = postUpload(t, a, cookie, formPart{"cover", "c.jpg", picked}, formPart{"file", "b.mp3", second})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "cover.jpg"))
	if !bytes.Equal(got, picked) || bytes.Equal(got, extracted) {
		t.Fatal("picked cover did not replace cover.jpg")
	}

	third := makeAudio(t, "c.mp3", true, "title", "Three", "artist", "Band", "album", "Covers", "track", "3")
	rec, _ = postUpload(t, a, cookie, formPart{"file", "c.mp3", third})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "cover.jpg")); !bytes.Equal(got, picked) {
		t.Fatal("embedded cover overwrote an existing cover.jpg")
	}

	pic := png(t)
	fourth := makeAudio(t, "d.mp3", false, "title", "Four", "artist", "Band", "album", "Covers", "track", "4")
	rec, _ = postUpload(t, a, cookie, formPart{"cover", "c.png", pic}, formPart{"file", "d.mp3", fourth})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "cover.png")); !bytes.Equal(got, pic) {
		t.Fatal("cover.png not written")
	}
	if _, err := os.Stat(filepath.Join(dir, "cover.jpg")); !os.IsNotExist(err) {
		t.Fatal("old cover.jpg kept next to the new cover.png")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".cover-") {
			t.Fatalf("temp cover left: %s", e.Name())
		}
	}
}

func TestTagWritingPerFormat(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	for _, name := range []string{"x.flac", "x.opus", "x.m4a", "x.mp3"} {
		data := makeAudio(t, name, name != "x.opus", "title", "Song "+name)
		rec, body := postUpload(t, a, cookie,
			formPart{"tags", "", []byte(`{"artist":"Fmt","album":"Formats","track":"1","year":"2021"}`)},
			formPart{"cover", "c.jpg", jpeg(t)},
			formPart{"file", name, data})
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		tags, err := probe(context.Background(), filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tags.Fields["artist"] != "Fmt" || tags.Fields["album"] != "Formats" || tags.Fields["year"] != "2021" {
			t.Fatalf("%s: tags %v", name, tags.Fields)
		}
		if name != "x.opus" && !tags.Cover {
			t.Fatalf("%s: embedded cover lost", name)
		}
	}
}

func TestScanDebounced(t *testing.T) {
	var calls atomic.Int32
	nav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "startScan.view") {
			calls.Add(1)
		}
		w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
	}))
	defer nav.Close()
	s := NewScanner(NewNavidrome(nav.URL), "scan", "pw", 30*time.Millisecond)
	for i := 0; i < 5; i++ {
		s.Trigger()
	}
	time.Sleep(150 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("scan calls %d", calls.Load())
	}
}
