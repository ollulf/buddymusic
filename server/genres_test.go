package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonGenre(t *testing.T) {
	cases := map[string]string{
		"Hip-Hop": "Hip Hop", "hiphop": "Hip Hop", " Soundtrack ": "Stage & Screen",
		"TECHNO": "Electronic", "Electronica": "Electronic", "R&B": "Funk / Soul",
		"Krautrock": "Krautrock", "": "",
		"Rock N Roll": "Rock & Roll", "Rock 'n' Roll": "Rock & Roll", "Rockabilly": "Rock & Roll", "Rock": "Rock",
		"Boogie-Woogie": "Boogie Woogie", "boogie woogie": "Boogie Woogie", "Swing": "Swing", "Big Band": "Swing", "Jazz": "Jazz",
	}
	for in, want := range cases {
		if got := canonGenre(in); got != want {
			t.Errorf("canonGenre(%q) = %q, want %q", in, got, want)
		}
	}
	for _, g := range genreList {
		if canonGenre(g) != g {
			t.Errorf("list genre %q not stable", g)
		}
	}
}

func genreNav(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.HasSuffix(r.URL.Path, "/getGenres.view"):
		w.Write([]byte(`{"subsonic-response":{"status":"ok","genres":{"genre":[{"value":"Techno","albumCount":2},{"value":"House","albumCount":3},{"value":"Hip-Hop","albumCount":4},{"value":"Krautrock","albumCount":1},{"value":"Empty","albumCount":0}]}}}`))
		return true
	case strings.HasSuffix(r.URL.Path, "/getAlbumList2.view"):
		if r.URL.Query().Get("size") != "500" {
			return false
		}
		w.Write([]byte(`{"subsonic-response":{"status":"ok","albumList2":{"album":[` +
			`{"id":"a1","genre":"Techno"},{"id":"a2","genres":[{"name":"Hip-Hop"}]},{"id":"a3","genre":"Jazz"},{"id":"a4","genre":"Rock; House"}]}}}`))
		return true
	}
	return false
}

func TestGenresEndpoint(t *testing.T) {
	a := newTestAppWith(t, genreNav)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/genres", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Genres []genreCount `json:"genres"`
		List   []string     `json:"list"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Genres) != 3 || got.Genres[0] != (genreCount{"Electronic", 5}) || got.Genres[1] != (genreCount{"Hip Hop", 4}) || got.Genres[2].Name != "Krautrock" {
		t.Fatalf("genres %+v", got.Genres)
	}
	if len(got.List) != len(genreList) {
		t.Fatalf("list %v", got.List)
	}
}

func TestAlbumsGenreFilter(t *testing.T) {
	a := newTestAppWith(t, genreNav)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/albums?type=newest&size=2&genre=Electronic&genre=hip-hop", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Album []struct {
			ID string `json:"id"`
		} `json:"album"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Album) != 2 || got.Album[0].ID != "a1" || got.Album[1].ID != "a2" {
		t.Fatalf("albums %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/albums?type=newest&genre=Rock", nil))
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Album) != 1 || got.Album[0].ID != "a4" {
		t.Fatalf("rock albums %s", rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/albums?type=newest", nil))
	if !strings.Contains(rec.Body.String(), `"al1"`) {
		t.Fatalf("unfiltered %s", rec.Body)
	}
}
