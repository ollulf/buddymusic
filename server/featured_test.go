package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fableSongs = `[
{"id":"s1","albumId":"alF","album":"Fable","artistId":"arR","artists":[{"id":"arR","name":"Russel Shaw"},{"id":"arD","name":"Danny Elfman"}],"albumArtists":[{"id":"arR","name":"Russel Shaw"}],"displayAlbumArtist":"Russel Shaw","coverArt":"al-alF","year":2015},
{"id":"s2","albumId":"alF","album":"Fable","artistId":"arR","artists":[{"id":"arR","name":"Russel Shaw"},{"id":"arD","name":"Danny Elfman"}],"albumArtists":[{"id":"arR","name":"Russel Shaw"}],"year":2015},
{"id":"s3","albumId":"alB","album":"Batman","artistId":"arD","artists":[{"id":"arD","name":"Danny Elfman"}],"albumArtists":[{"id":"arD","name":"Danny Elfman"}],"year":1989},
{"id":"s4","albumId":"alX","album":"Duo","artistId":"arA","artists":[{"id":"arA","name":"Ann"},{"id":"arZ","name":"zed guest"}],"albumArtists":[{"id":"arA","name":"Ann"}],"year":2020}
]`

func TestFeaturedFrom(t *testing.T) {
	var songs []scanSong
	if err := json.Unmarshal([]byte(fableSongs), &songs); err != nil {
		t.Fatal(err)
	}
	feat := featuredFrom(songs)
	if len(feat) != 2 || feat["arR"] != nil || feat["arA"] != nil {
		t.Fatalf("featured %v", feat)
	}
	d := feat["arD"]
	if d == nil || d.Name != "Danny Elfman" || len(d.Albums) != 1 {
		t.Fatalf("danny %+v", d)
	}
	al := d.Albums[0]
	if al.ID != "alF" || al.Name != "Fable" || al.Artist != "Russel Shaw" || al.ArtistID != "arR" || al.CoverArt != "al-alF" || al.Year != 2015 {
		t.Fatalf("album %+v", al)
	}
}

func TestMergeFeaturedIndex(t *testing.T) {
	feat := map[string]*featuredArtist{
		"arD": {ID: "arD", Name: "Danny Elfman", Albums: []featuredAlbum{{ID: "alF"}}},
		"arZ": {ID: "arZ", Name: "zed guest", Albums: []featuredAlbum{{ID: "alX"}}},
		"arT": {ID: "arT", Name: "The Band", Albums: []featuredAlbum{{ID: "alX"}}},
		"arN": {ID: "arN", Name: "99 Luft", Albums: []featuredAlbum{{ID: "alX"}}},
		"arR": {ID: "arR", Name: "Russel Shaw"},
	}
	raw := json.RawMessage(`{"ignoredArticles":"The El","index":[{"name":"R","artist":[{"id":"arR","name":"Russel Shaw","albumCount":1}]},{"name":"D","artist":[{"id":"arDa","name":"Dido","albumCount":2}]}]}`)
	out, err := mergeFeaturedIndex(raw, feat)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Ignored string `json:"ignoredArticles"`
		Index   []struct {
			Name   string `json:"name"`
			Artist []struct {
				ID       string `json:"id"`
				Featured bool   `json:"featured"`
			} `json:"artist"`
		} `json:"index"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range doc.Index {
		ids := []string{}
		for _, a := range g.Artist {
			ids = append(ids, a.ID)
		}
		got = append(got, g.Name+":"+strings.Join(ids, ","))
	}
	want := "B:arT D:arD,arDa R:arR Z:arZ #:arN"
	if strings.Join(got, " ") != want || doc.Ignored != "The El" {
		t.Fatalf("index %q", strings.Join(got, " "))
	}
	if !doc.Index[1].Artist[0].Featured || doc.Index[1].Artist[1].Featured {
		t.Fatalf("featured flags %+v", doc.Index[1])
	}
}

func featuredNav(w http.ResponseWriter, r *http.Request) bool {
	q := r.URL.Query()
	switch {
	case strings.HasSuffix(r.URL.Path, "/search3.view"):
		w.Write([]byte(`{"subsonic-response":{"status":"ok","searchResult3":{"song":` + fableSongs + `}}}`))
	case strings.HasSuffix(r.URL.Path, "/getArtists.view"):
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artists":{"ignoredArticles":"The","index":[{"name":"R","artist":[{"id":"arR","name":"Russel Shaw","albumCount":1}]}]}}}`))
	case strings.HasSuffix(r.URL.Path, "/getArtist.view") && (q.Get("id") == "arZ" || q.Get("id") == "nobody"):
		w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":70,"message":"not found"}}}`))
	case strings.HasSuffix(r.URL.Path, "/getArtist.view"):
		w.Write([]byte(`{"subsonic-response":{"status":"ok","artist":{"id":"` + q.Get("id") + `","name":"X","album":[{"id":"alB","name":"Batman"}]}}}`))
	default:
		return false
	}
	return true
}

func TestFeaturedArtistRoutes(t *testing.T) {
	a := newTestAppWith(t, featuredNav)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/music/artists", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"arD"`) || !strings.Contains(rec.Body.String(), `"id":"arZ"`) {
		t.Fatalf("artists %d: %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/artist/arD", nil))
	var ar struct {
		Album     []featuredAlbum `json:"album"`
		AppearsOn []featuredAlbum `json:"appearsOn"`
	}
	json.Unmarshal(rec.Body.Bytes(), &ar)
	if rec.Code != 200 || len(ar.Album) != 1 || len(ar.AppearsOn) != 1 || ar.AppearsOn[0].ID != "alF" {
		t.Fatalf("artist %d: %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/artist/arZ", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"zed guest"`) || !strings.Contains(rec.Body.String(), `"id":"alX"`) {
		t.Fatalf("featured-only artist %d: %s", rec.Code, rec.Body)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/artist/nobody", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown artist %d", rec.Code)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/music/artist/bad!id", nil))
	if rec.Code != 400 {
		t.Fatalf("bad id %d", rec.Code)
	}
}
