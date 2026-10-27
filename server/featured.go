package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type idName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type scanSong struct {
	ID                 string   `json:"id"`
	AlbumID            string   `json:"albumId"`
	Album              string   `json:"album"`
	Artist             string   `json:"artist"`
	ArtistID           string   `json:"artistId"`
	Artists            []idName `json:"artists"`
	AlbumArtists       []idName `json:"albumArtists"`
	DisplayAlbumArtist string   `json:"displayAlbumArtist"`
	CoverArt           string   `json:"coverArt"`
	Year               int      `json:"year"`
}

type featuredAlbum struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Artist   string `json:"artist,omitempty"`
	ArtistID string `json:"artistId,omitempty"`
	CoverArt string `json:"coverArt,omitempty"`
	Year     int    `json:"year,omitempty"`
}

type featuredArtist struct {
	ID     string
	Name   string
	Albums []featuredAlbum
}

const scanPage = 500
const scanMaxPages = 200

var featuredTTL = 2 * time.Minute

type featuredCache struct {
	mu    sync.Mutex
	users map[string]featuredEntry
}

type featuredEntry struct {
	at  time.Time
	out map[string]*featuredArtist
}

func featuredFrom(songs []scanSong) map[string]*featuredArtist {
	out := map[string]*featuredArtist{}
	seen := map[string]bool{}
	for _, s := range songs {
		if s.AlbumID == "" {
			continue
		}
		own := map[string]bool{}
		for _, a := range s.AlbumArtists {
			own[a.ID] = true
			own["n:"+strings.ToLower(strings.TrimSpace(a.Name))] = true
		}
		if len(s.AlbumArtists) == 0 && s.ArtistID != "" {
			own[s.ArtistID] = true
		}
		for _, a := range s.Artists {
			name := strings.TrimSpace(a.Name)
			if a.ID == "" || name == "" || own[a.ID] || own["n:"+strings.ToLower(name)] {
				continue
			}
			fa := out[a.ID]
			if fa == nil {
				fa = &featuredArtist{ID: a.ID, Name: name}
				out[a.ID] = fa
			}
			key := a.ID + "|" + s.AlbumID
			if seen[key] {
				continue
			}
			seen[key] = true
			al := featuredAlbum{ID: s.AlbumID, Name: s.Album, CoverArt: s.CoverArt, Year: s.Year, Artist: s.DisplayAlbumArtist}
			if len(s.AlbumArtists) > 0 {
				al.ArtistID = s.AlbumArtists[0].ID
				if al.Artist == "" {
					names := make([]string, 0, len(s.AlbumArtists))
					for _, x := range s.AlbumArtists {
						names = append(names, x.Name)
					}
					al.Artist = strings.Join(names, "; ")
				}
			}
			if al.CoverArt == "" {
				al.CoverArt = s.AlbumID
			}
			fa.Albums = append(fa.Albums, al)
		}
	}
	for _, fa := range out {
		sort.SliceStable(fa.Albums, func(i, j int) bool {
			if fa.Albums[i].Year != fa.Albums[j].Year {
				return fa.Albums[i].Year > fa.Albums[j].Year
			}
			return strings.ToLower(fa.Albums[i].Name) < strings.ToLower(fa.Albums[j].Name)
		})
	}
	return out
}

func (a *App) featured(ctx context.Context, user string, creds SubsonicCreds) (map[string]*featuredArtist, error) {
	a.feat.mu.Lock()
	if e, ok := a.feat.users[user]; ok && time.Since(e.at) < featuredTTL {
		a.feat.mu.Unlock()
		return e.out, nil
	}
	a.feat.mu.Unlock()
	var songs []scanSong
	for page := 0; page < scanMaxPages; page++ {
		q := url.Values{"query": {""}, "artistCount": {"0"}, "albumCount": {"0"},
			"songCount": {strconv.Itoa(scanPage)}, "songOffset": {strconv.Itoa(page * scanPage)}}
		raw, err := a.nav.raw(ctx, "search3.view", "searchResult3", creds, q)
		if err != nil {
			return nil, err
		}
		var res struct {
			Song []scanSong `json:"song"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, err
		}
		songs = append(songs, res.Song...)
		if len(res.Song) < scanPage {
			break
		}
	}
	out := featuredFrom(songs)
	a.feat.mu.Lock()
	if a.feat.users == nil {
		a.feat.users = map[string]featuredEntry{}
	}
	a.feat.users[user] = featuredEntry{at: time.Now(), out: out}
	a.feat.mu.Unlock()
	return out, nil
}

func indexLetter(name string, articles []string) string {
	n := strings.TrimSpace(name)
	low := strings.ToLower(n)
	for _, art := range articles {
		if art != "" && strings.HasPrefix(low, strings.ToLower(art)+" ") {
			n = strings.TrimSpace(n[len(art):])
			break
		}
	}
	for _, r := range n {
		if unicode.IsLetter(r) {
			return strings.ToUpper(string(r))
		}
		return "#"
	}
	return "#"
}

func mergeFeaturedIndex(raw json.RawMessage, feat map[string]*featuredArtist) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var groups []struct {
		Name   string                   `json:"name"`
		Artist []map[string]interface{} `json:"artist"`
	}
	if v, ok := doc["index"]; ok {
		if err := json.Unmarshal(v, &groups); err != nil {
			return nil, err
		}
	}
	var ignored string
	json.Unmarshal(doc["ignoredArticles"], &ignored)
	articles := strings.Fields(ignored)
	known := map[string]bool{}
	byName := map[string]int{}
	for i, g := range groups {
		byName[g.Name] = i
		for _, ar := range g.Artist {
			if id, _ := ar["id"].(string); id != "" {
				known[id] = true
			}
		}
	}
	added := false
	for _, fa := range feat {
		if known[fa.ID] {
			continue
		}
		letter := indexLetter(fa.Name, articles)
		i, ok := byName[letter]
		if !ok {
			groups = append(groups, struct {
				Name   string                   `json:"name"`
				Artist []map[string]interface{} `json:"artist"`
			}{Name: letter})
			i = len(groups) - 1
			byName[letter] = i
		}
		groups[i].Artist = append(groups[i].Artist, map[string]interface{}{
			"id": fa.ID, "name": fa.Name, "albumCount": 0, "featured": true, "appearsOnCount": len(fa.Albums),
		})
		added = true
	}
	if !added {
		return raw, nil
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].Name, groups[j].Name
		if (a == "#") != (b == "#") {
			return b == "#"
		}
		return a < b
	})
	for _, g := range groups {
		sort.SliceStable(g.Artist, func(i, j int) bool {
			ni, _ := g.Artist[i]["name"].(string)
			nj, _ := g.Artist[j]["name"].(string)
			return strings.ToLower(ni) < strings.ToLower(nj)
		})
	}
	b, err := json.Marshal(groups)
	if err != nil {
		return nil, err
	}
	doc["index"] = b
	return json.Marshal(doc)
}

func addAppearsOn(raw json.RawMessage, fa *featuredArtist) (json.RawMessage, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var own []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(doc["album"], &own)
	skip := map[string]bool{}
	for _, al := range own {
		skip[al.ID] = true
	}
	list := []featuredAlbum{}
	for _, al := range fa.Albums {
		if !skip[al.ID] {
			list = append(list, al)
		}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	doc["appearsOn"] = b
	return json.Marshal(doc)
}

func writeMusicError(w http.ResponseWriter, endpoint string, err error) {
	switch {
	case errors.Is(err, ErrBadLogin):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not-found")
	default:
		log.Printf("music %s: %v", endpoint, err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
	}
}

func writeRawJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

func (a *App) handleArtists(w http.ResponseWriter, r *http.Request, user string) {
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := a.nav.raw(r.Context(), "getArtists.view", "artists", creds, nil)
	if err != nil {
		writeMusicError(w, "getArtists.view", err)
		return
	}
	feat, err := a.featured(r.Context(), user, creds)
	if err != nil {
		log.Printf("featured artists: %v", err)
		writeRawJSON(w, body)
		return
	}
	if merged, err := mergeFeaturedIndex(body, feat); err == nil {
		body = merged
	}
	writeRawJSON(w, body)
}

func (a *App) handleArtist(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, navErr := a.nav.raw(r.Context(), "getArtist.view", "artist", creds, url.Values{"id": {id}})
	if navErr != nil && !errors.Is(navErr, ErrNotFound) {
		writeMusicError(w, "getArtist.view", navErr)
		return
	}
	feat, err := a.featured(r.Context(), user, creds)
	if err != nil {
		log.Printf("featured artists: %v", err)
	}
	fa := feat[id]
	if navErr != nil {
		if fa == nil {
			writeMusicError(w, "getArtist.view", navErr)
			return
		}
		body, _ = json.Marshal(map[string]interface{}{"id": fa.ID, "name": fa.Name, "album": []interface{}{}})
	}
	if fa != nil {
		if out, err := addAppearsOn(body, fa); err == nil {
			body = out
		}
	}
	writeRawJSON(w, body)
}
