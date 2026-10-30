package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTypedArtists(t *testing.T) {
	typed, err := parseTyped([]byte(`{"artists":[" Main ","Guest; Two","main","",5,"C","D","E","F","G","H","I","J"],"albumArtists":["Main"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if typed["artist"] != "Main" {
		t.Fatalf("main artist %q", typed["artist"])
	}
	if typed["artists"] != "Main; Guest, Two; C; D; E; F; G; H" {
		t.Fatalf("artists %q", typed["artists"])
	}
	if typed["albumartist"] != "Main" {
		t.Fatalf("album artist %q", typed["albumartist"])
	}
	single, _ := parseTyped([]byte(`{"artists":["Solo"],"albumArtists":["Solo"]}`))
	if !reflect.DeepEqual(single, map[string]string{"artist": "Solo"}) {
		t.Fatalf("single %v", single)
	}
	duo, _ := parseTyped([]byte(`{"artists":["A","B"],"albumArtists":["A","B"]}`))
	if duo["albumartist"] != "A; B" || duo["artist"] != "A" {
		t.Fatalf("duo %v", duo)
	}
}

func rawTags(t *testing.T, path string) map[string]string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", "--", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for _, s := range p.Streams {
		for k, v := range s.Tags {
			tags[strings.ToLower(k)] = v
		}
	}
	for k, v := range p.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	return tags
}

func TestUploadMultipleArtists(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	for _, name := range []string{"m.flac", "m.mp3"} {
		data := makeAudio(t, name, false, "title", "Duet "+name)
		rec, body := postUpload(t, a, cookie,
			formPart{"tags", "", []byte(`{"artists":["Lead","Guest"],"albumArtists":["Lead","Partner"],"album":"Together","track":"4"}`)},
			formPart{"cover", "c.jpg", jpeg(t)},
			formPart{"file", name, data})
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		if !strings.HasPrefix(body["libraryPath"].(string), "max/Lead/Together/04 - Duet") {
			t.Fatalf("%s: folder not from first artist: %v", name, body["libraryPath"])
		}
		if body["artist"] != "Lead" {
			t.Fatalf("%s: upload artist %v", name, body["artist"])
		}
		tags := rawTags(t, filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string)))
		if tags["artist"] != "Lead; Guest" {
			t.Fatalf("%s: artist tag %v", name, tags)
		}
		if got := tags["album_artist"] + tags["albumartist"]; got != "Lead; Partner" {
			t.Fatalf("%s: album artist tag %v", name, tags)
		}
	}
}
