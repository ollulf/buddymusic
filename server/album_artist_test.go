package main

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestUploadGuestOnOneSongKeepsAlbumWhole(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	songs := []struct {
		track, tags, wantArtist string
	}{
		{"1", `{"artist":"Russel Shaw","albumArtists":["Russel Shaw"],"artists":["Russel Shaw","Danny Elfman"],"album":"Fable","track":"1","title":"One"}`, "Russel Shaw; Danny Elfman"},
		{"2", `{"artist":"Russel Shaw","album":"Fable","track":"2","title":"Two"}`, "Russel Shaw"},
		{"3", `{"artist":"Russel Shaw","album":"Fable","track":"3","title":"Three"}`, "Russel Shaw"},
	}
	for _, s := range songs {
		data := makeAudio(t, "s.flac", false, "album_artist", "Russel Shaw, Danny Elfman", "artist", "Russel Shaw")
		rec, body := postUpload(t, a, cookie,
			formPart{"tags", "", []byte(s.tags)},
			formPart{"cover", "c.jpg", jpeg(t)},
			formPart{"file", "s.flac", data})
		if rec.Code != http.StatusCreated {
			t.Fatalf("track %s: %d %s", s.track, rec.Code, rec.Body)
		}
		tags := rawTags(t, filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string)))
		if tags["artist"] != s.wantArtist {
			t.Fatalf("track %s: artist %q", s.track, tags["artist"])
		}
		if got := tags["album_artist"] + tags["albumartist"]; got != "Russel Shaw" {
			t.Fatalf("track %s: album artist %q", s.track, got)
		}
	}
}

func TestLaterUploadReusesAlbumArtist(t *testing.T) {
	needFFmpeg(t)
	a := newTestApp(t)
	cookie := login(t, a)
	upload := func(tags string) map[string]string {
		rec, body := postUpload(t, a, cookie,
			formPart{"tags", "", []byte(tags)},
			formPart{"cover", "c.jpg", jpeg(t)},
			formPart{"file", "s.mp3", makeAudio(t, "s.mp3", false)})
		if rec.Code != http.StatusCreated {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return rawTags(t, filepath.Join(a.cfg.LibraryDir(), body["libraryPath"].(string)))
	}
	upload(`{"artists":["Lead","Partner"],"albumArtists":["Lead","Partner"],"album":"Pair","track":"1","title":"A"}`)
	later := upload(`{"artist":"Lead","album":"Pair","track":"2","title":"B"}`)
	if got := later["album_artist"] + later["albumartist"]; got != "Lead; Partner" {
		t.Fatalf("album artist %q", got)
	}
	if later["artist"] != "Lead" {
		t.Fatalf("artist %q", later["artist"])
	}
}

func TestChooseAlbumArtist(t *testing.T) {
	if got := chooseAlbumArtist(map[string]string{"albumartist": "A; B"}, nil, "A"); got != "A; B" {
		t.Fatalf("explicit %q", got)
	}
	if got := chooseAlbumArtist(map[string]string{"albumartist": "A"}, nil, "A, B"); got != "A, B" {
		t.Fatalf("existing %q", got)
	}
	if got := chooseAlbumArtist(map[string]string{}, map[string]string{"artist": "A"}, ""); got != "A" {
		t.Fatalf("main %q", got)
	}
}
