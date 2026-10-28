package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
)

type playlistEntry struct {
	AlbumID  string `json:"albumId"`
	CoverArt string `json:"coverArt"`
}

func playlistCovers(entries []playlistEntry, limit int) []string {
	covers := []string{}
	seen := map[string]bool{}
	for _, e := range entries {
		key := e.AlbumID
		if key == "" {
			key = e.CoverArt
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		id := e.CoverArt
		if id == "" {
			id = e.AlbumID
		}
		covers = append(covers, id)
		if len(covers) == limit {
			break
		}
	}
	return covers
}

func (a *App) handlePlaylists(w http.ResponseWriter, r *http.Request, user string) {
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := a.nav.raw(r.Context(), "getPlaylists.view", "playlists", creds, nil)
	if err != nil {
		writeNavError(w, "getPlaylists.view", err)
		return
	}
	var list struct {
		Playlist []map[string]json.RawMessage `json:"playlist"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		writeNavError(w, "getPlaylists.view", err)
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, pl := range list.Playlist {
		var id string
		json.Unmarshal(pl["id"], &id)
		pl["covers"] = json.RawMessage("[]")
		if !validID(id) {
			continue
		}
		wg.Add(1)
		go func(pl map[string]json.RawMessage, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := a.nav.raw(r.Context(), "getPlaylist.view", "playlist", creds, url.Values{"id": {id}})
			if err != nil {
				return
			}
			var full struct {
				Entry []playlistEntry `json:"entry"`
			}
			if json.Unmarshal(raw, &full) != nil {
				return
			}
			if b, err := json.Marshal(playlistCovers(full.Entry, 5)); err == nil {
				pl["covers"] = b
			}
		}(pl, id)
	}
	wg.Wait()
	if list.Playlist == nil {
		list.Playlist = []map[string]json.RawMessage{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(list)
}
