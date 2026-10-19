package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type playlistInfo struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	SongCount int    `json:"songCount"`
}

func writeNavError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, ErrBadLogin):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not-found")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "not-owner")
	default:
		log.Printf("music %s: %v", what, err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return false
	}
	return true
}

func cleanName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && len(s) <= 200
}

func validIDs(ids []string) bool {
	if len(ids) > 5000 {
		return false
	}
	for _, id := range ids {
		if !validID(id) {
			return false
		}
	}
	return true
}

func (a *App) ownPlaylist(w http.ResponseWriter, r *http.Request, user string) (SubsonicCreds, playlistInfo, bool) {
	var info playlistInfo
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return SubsonicCreds{}, info, false
	}
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return creds, info, false
	}
	body, err := a.nav.raw(r.Context(), "getPlaylist.view", "playlist", creds, url.Values{"id": {id}})
	if err != nil {
		writeNavError(w, "getPlaylist.view", err)
		return creds, info, false
	}
	if err := json.Unmarshal(body, &info); err != nil || info.ID == "" {
		writeError(w, http.StatusNotFound, "not-found")
		return creds, info, false
	}
	if !strings.EqualFold(info.Owner, user) {
		writeError(w, http.StatusForbidden, "not-owner")
		return creds, info, false
	}
	return creds, info, true
}

func (a *App) handlePlaylistCreate(w http.ResponseWriter, r *http.Request, user string) {
	var in struct {
		Name    string   `json:"name"`
		SongIDs []string `json:"songIds"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name, ok := cleanName(in.Name)
	if !ok || !validIDs(in.SongIDs) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := a.nav.raw(r.Context(), "createPlaylist.view", "playlist", creds, url.Values{"name": {name}, "songId": in.SongIDs})
	if err != nil {
		writeNavError(w, "createPlaylist.view", err)
		return
	}
	var info playlistInfo
	json.Unmarshal(body, &info)
	if info.ID != "" {
		if _, err := a.nav.raw(r.Context(), "updatePlaylist.view", "", creds, url.Values{"playlistId": {info.ID}, "public": {"false"}}); err != nil {
			log.Printf("music make playlist private: %v", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	w.Write(body)
}

func (a *App) handlePlaylistUpdate(w http.ResponseWriter, r *http.Request, user string) {
	var in struct {
		Name   *string `json:"name"`
		Public *bool   `json:"public"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	q := url.Values{}
	if in.Name != nil {
		name, ok := cleanName(*in.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		q.Set("name", name)
	}
	if in.Public != nil {
		q.Set("public", strconv.FormatBool(*in.Public))
	}
	creds, info, ok := a.ownPlaylist(w, r, user)
	if !ok {
		return
	}
	q.Set("playlistId", info.ID)
	if _, err := a.nav.raw(r.Context(), "updatePlaylist.view", "", creds, q); err != nil {
		writeNavError(w, "updatePlaylist.view", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": info.ID})
}

func (a *App) handlePlaylistDelete(w http.ResponseWriter, r *http.Request, user string) {
	creds, info, ok := a.ownPlaylist(w, r, user)
	if !ok {
		return
	}
	if _, err := a.nav.raw(r.Context(), "deletePlaylist.view", "", creds, url.Values{"id": {info.ID}}); err != nil {
		writeNavError(w, "deletePlaylist.view", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handlePlaylistSongs(replace bool) userHandler {
	return func(w http.ResponseWriter, r *http.Request, user string) {
		var in struct {
			SongIDs []string `json:"songIds"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if !validIDs(in.SongIDs) || (!replace && len(in.SongIDs) == 0) {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		creds, info, ok := a.ownPlaylist(w, r, user)
		if !ok {
			return
		}
		q := url.Values{"playlistId": {info.ID}, "songIdToAdd": in.SongIDs}
		if replace {
			for i := 0; i < info.SongCount; i++ {
				q.Add("songIndexToRemove", strconv.Itoa(i))
			}
		}
		if _, err := a.nav.raw(r.Context(), "updatePlaylist.view", "", creds, q); err != nil {
			writeNavError(w, "updatePlaylist.view", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": info.ID})
	}
}
