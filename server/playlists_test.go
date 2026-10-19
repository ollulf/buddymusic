package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type fakePlaylist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Owner  string   `json:"owner"`
	Public bool     `json:"public"`
	Songs  []string `json:"-"`
	Count  int      `json:"songCount"`
}

func fakePlaylists() func(w http.ResponseWriter, r *http.Request) bool {
	var mu sync.Mutex
	lists := map[string]*fakePlaylist{
		"pl-boss-pub":  {ID: "pl-boss-pub", Name: "Boss hits", Owner: "boss", Public: true, Songs: []string{"s1"}},
		"pl-boss-priv": {ID: "pl-boss-priv", Name: "Boss secret", Owner: "boss"},
	}
	next := 0
	reply := func(w http.ResponseWriter, key string, v any) {
		body := map[string]any{"status": "ok"}
		if key != "" {
			body[key] = v
		}
		json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}
	fail := func(w http.ResponseWriter, code int) {
		w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":` + strconv.Itoa(code) + `,"message":"x"}}}`))
	}
	view := func(p *fakePlaylist) map[string]any {
		entries := []map[string]string{}
		for _, s := range p.Songs {
			entries = append(entries, map[string]string{"id": s})
		}
		return map[string]any{"id": p.ID, "name": p.Name, "owner": p.Owner, "public": p.Public, "songCount": len(p.Songs), "entry": entries}
	}
	return func(w http.ResponseWriter, r *http.Request) bool {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		u := q.Get("u")
		ep := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch ep {
		case "getPlaylists.view":
			var out []map[string]any
			ids := []string{}
			for id := range lists {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				p := lists[id]
				if p.Owner == u || p.Public {
					out = append(out, view(p))
				}
			}
			reply(w, "playlists", map[string]any{"playlist": out})
		case "getPlaylist.view":
			p := lists[q.Get("id")]
			if p == nil || (p.Owner != u && !p.Public) {
				fail(w, 70)
				return true
			}
			reply(w, "playlist", view(p))
		case "createPlaylist.view":
			next++
			p := &fakePlaylist{ID: "pl" + strconv.Itoa(next), Name: q.Get("name"), Owner: u, Public: true, Songs: q["songId"]}
			lists[p.ID] = p
			reply(w, "playlist", view(p))
		case "updatePlaylist.view":
			p := lists[q.Get("playlistId")]
			if p == nil {
				fail(w, 70)
				return true
			}
			if p.Owner != u {
				fail(w, 50)
				return true
			}
			if q.Has("name") {
				p.Name = q.Get("name")
			}
			if q.Has("public") {
				p.Public = q.Get("public") == "true"
			}
			drop := map[int]bool{}
			for _, s := range q["songIndexToRemove"] {
				i, _ := strconv.Atoi(s)
				drop[i] = true
			}
			kept := []string{}
			for i, s := range p.Songs {
				if !drop[i] {
					kept = append(kept, s)
				}
			}
			p.Songs = append(kept, q["songIdToAdd"]...)
			reply(w, "", nil)
		case "deletePlaylist.view":
			p := lists[q.Get("id")]
			if p == nil {
				fail(w, 70)
				return true
			}
			if p.Owner != u {
				fail(w, 50)
				return true
			}
			delete(lists, p.ID)
			reply(w, "", nil)
		default:
			return false
		}
		return true
	}
}

func asUser(t *testing.T, a *App, c *http.Cookie, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(c)
	return do(t, a.Routes(), req)
}

func TestPlaylistsFlow(t *testing.T) {
	a := newTestAppWith(t, fakePlaylists())
	max := loginAs(t, a, "max", "secret")
	boss := loginAs(t, a, "boss", "bosspw")

	rec := asUser(t, a, max, "POST", "/api/music/playlists", `{"name":"  Road trip ","songIds":["s1","s2"]}`)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"id":"pl1"`) {
		t.Fatalf("create %d: %s", rec.Code, rec.Body)
	}
	rec = asUser(t, a, max, "GET", "/api/music/playlist/pl1", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"Road trip"`) || !strings.Contains(rec.Body.String(), `"public":false`) {
		t.Fatalf("new playlist not private: %d %s", rec.Code, rec.Body)
	}

	rec = asUser(t, a, max, "GET", "/api/music/playlists", "")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `"id":"pl1"`) || !strings.Contains(body, `"id":"pl-boss-pub"`) || strings.Contains(body, "pl-boss-priv") {
		t.Fatalf("list %d: %s", rec.Code, body)
	}

	rec = asUser(t, a, max, "PATCH", "/api/music/playlist/pl1", `{"name":"Trip","public":true}`)
	if rec.Code != 200 {
		t.Fatalf("patch %d: %s", rec.Code, rec.Body)
	}
	rec = asUser(t, a, boss, "GET", "/api/music/playlists", "")
	if !strings.Contains(rec.Body.String(), `"name":"Trip"`) {
		t.Fatalf("public playlist not visible to others: %s", rec.Body)
	}

	rec = asUser(t, a, max, "POST", "/api/music/playlist/pl1/songs", `{"songIds":["s3"]}`)
	if rec.Code != 200 {
		t.Fatalf("append %d: %s", rec.Code, rec.Body)
	}
	rec = asUser(t, a, max, "PUT", "/api/music/playlist/pl1/songs", `{"songIds":["s3","s1"]}`)
	if rec.Code != 200 {
		t.Fatalf("replace %d: %s", rec.Code, rec.Body)
	}
	rec = asUser(t, a, max, "GET", "/api/music/playlist/pl1", "")
	if !strings.Contains(rec.Body.String(), `"entry":[{"id":"s3"},{"id":"s1"}]`) {
		t.Fatalf("songs after replace: %s", rec.Body)
	}

	for _, c := range []struct{ method, target, body string }{
		{"PATCH", "/api/music/playlist/pl-boss-pub", `{"name":"mine now"}`},
		{"DELETE", "/api/music/playlist/pl-boss-pub", ""},
		{"PUT", "/api/music/playlist/pl-boss-pub/songs", `{"songIds":[]}`},
		{"POST", "/api/music/playlist/pl-boss-pub/songs", `{"songIds":["s2"]}`},
	} {
		if rec := asUser(t, a, max, c.method, c.target, c.body); rec.Code != 403 {
			t.Fatalf("%s %s by non-owner: %d %s", c.method, c.target, rec.Code, rec.Body)
		}
	}
	if rec := asUser(t, a, max, "DELETE", "/api/music/playlist/pl-boss-priv", ""); rec.Code != 404 {
		t.Fatalf("delete hidden playlist: %d", rec.Code)
	}

	if rec := asUser(t, a, max, "DELETE", "/api/music/playlist/pl1", ""); rec.Code != 204 {
		t.Fatalf("delete own %d: %s", rec.Code, rec.Body)
	}
	if rec := asUser(t, a, max, "GET", "/api/music/playlist/pl1", ""); rec.Code != 404 {
		t.Fatalf("deleted playlist still there: %d", rec.Code)
	}
}

func TestPlaylistsValidation(t *testing.T) {
	a := newTestAppWith(t, fakePlaylists())
	max := loginAs(t, a, "max", "secret")
	for _, c := range []struct{ method, target, body string }{
		{"POST", "/api/music/playlists", `{"name":"   "}`},
		{"POST", "/api/music/playlists", `{"name":"x","songIds":["../etc"]}`},
		{"POST", "/api/music/playlists", `not json`},
		{"PATCH", "/api/music/playlist/pl-boss-pub", `{"name":""}`},
		{"POST", "/api/music/playlist/pl-boss-pub/songs", `{"songIds":[]}`},
	} {
		if rec := asUser(t, a, max, c.method, c.target, c.body); rec.Code != 400 {
			t.Fatalf("%s %s %q: %d", c.method, c.target, c.body, rec.Code)
		}
	}
	req := httptest.NewRequest("GET", "/api/music/playlists", nil)
	if rec := do(t, a.Routes(), req); rec.Code != 401 {
		t.Fatalf("anon list %d", rec.Code)
	}
}
