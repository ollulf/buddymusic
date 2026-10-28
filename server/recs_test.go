package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecsHandlers(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	call := func(c *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(c)
		return do(t, a.Routes(), req)
	}
	me := func(c *http.Cookie) (string, int) {
		var out struct {
			User       string `json:"user"`
			RecsUnread int    `json:"recsUnread"`
		}
		json.Unmarshal(call(c, "GET", "/api/me", "").Body.Bytes(), &out)
		return out.User, out.RecsUnread
	}
	maxName, _ := me(max)
	bossName, unread := me(boss)
	if unread != 0 {
		t.Fatalf("unread before: %d", unread)
	}

	send := func(to, note string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"song": map[string]any{"id": "s1", "title": "Song", "artist": "Band", "albumId": "al1", "coverArt": "c1", "duration": 200}, "to": []string{to}, "note": note})
		return call(max, "POST", "/api/recs", string(b))
	}
	if rec := send(maxName, ""); rec.Code != 400 {
		t.Fatalf("self: %d", rec.Code)
	}
	if rec := send("ghost", ""); rec.Code != 400 {
		t.Fatalf("unknown: %d", rec.Code)
	}
	if rec := send(bossName, strings.Repeat("x", 141)); rec.Code != 400 {
		t.Fatalf("long note: %d", rec.Code)
	}
	if rec := send(bossName, "listen to this"); rec.Code != 200 {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	if _, n := me(boss); n != 1 {
		t.Fatalf("unread after send: %d", n)
	}

	var list struct {
		Recs   []Rec `json:"recs"`
		Unread int   `json:"unread"`
	}
	rec := call(boss, "GET", "/api/recs", "")
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Recs) != 1 || list.Unread != 1 {
		t.Fatalf("list: %s", rec.Body)
	}
	r := list.Recs[0]
	if r.From != maxName || r.Song.ID != "s1" || r.Song.Title != "Song" || r.Note != "listen to this" || r.Seen {
		t.Fatalf("rec: %+v", r)
	}
	if rec := call(max, "GET", "/api/recs", ""); strings.Contains(rec.Body.String(), "s1") {
		t.Fatalf("sender sees own rec: %s", rec.Body)
	}
	if rec := call(max, "POST", "/api/recs/"+r.ID, `{"seen":true}`); rec.Code != 404 {
		t.Fatalf("other user marks: %d", rec.Code)
	}
	if rec := call(boss, "POST", "/api/recs/all", `{"seen":true}`); rec.Code != 200 {
		t.Fatalf("seen all: %d", rec.Code)
	}
	if _, n := me(boss); n != 0 {
		t.Fatalf("unread after seen: %d", n)
	}
	if rec := call(boss, "POST", "/api/recs/"+r.ID, `{"dismiss":true}`); rec.Code != 200 {
		t.Fatalf("dismiss: %d", rec.Code)
	}
	json.Unmarshal(call(boss, "GET", "/api/recs", "").Body.Bytes(), &list)
	if len(list.Recs) != 0 {
		t.Fatalf("after dismiss: %+v", list.Recs)
	}
}

func TestRecsRateLimit(t *testing.T) {
	a := newTestApp(t)
	s := a.store
	now := time.Now()
	s.TouchSeen("ann", now, "")
	song := RecSong{ID: "s1"}
	for i := 0; i < recPerHour; i++ {
		if _, err := s.SendRecs("max", []string{"ann"}, song, "", now); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if _, err := s.SendRecs("max", []string{"ann"}, song, "", now); err != errRecLimit {
		t.Fatalf("want limit, got %v", err)
	}
	if _, err := s.SendRecs("max", []string{"ann"}, song, "", now.Add(2*time.Hour)); err != nil {
		t.Fatalf("after an hour: %v", err)
	}
}

func TestAlbumRecs(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	call := func(c *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(c)
		return do(t, a.Routes(), req)
	}
	var me struct{ User string }
	json.Unmarshal(call(boss, "GET", "/api/me", "").Body.Bytes(), &me)
	send := func(body map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		return call(max, "POST", "/api/recs", string(b))
	}
	if rec := send(map[string]any{"kind": "album", "album": map[string]any{"id": ""}, "to": []string{me.User}}); rec.Code != 400 {
		t.Fatalf("no album id: %d", rec.Code)
	}
	if rec := send(map[string]any{"kind": "video", "song": map[string]any{"id": "s1"}, "to": []string{me.User}}); rec.Code != 400 {
		t.Fatalf("bad kind: %d", rec.Code)
	}
	if rec := send(map[string]any{"kind": "album", "album": map[string]any{"id": "al1", "name": "Record", "artist": "Band", "coverArt": "c1"}, "to": []string{me.User}, "note": "whole thing"}); rec.Code != 200 {
		t.Fatalf("send album: %d %s", rec.Code, rec.Body)
	}
	if rec := send(map[string]any{"song": map[string]any{"id": "s1", "title": "Song"}, "to": []string{me.User}}); rec.Code != 200 {
		t.Fatalf("send song: %d %s", rec.Code, rec.Body)
	}
	var list struct {
		Recs   []Rec `json:"recs"`
		Unread int   `json:"unread"`
	}
	json.Unmarshal(call(boss, "GET", "/api/recs", "").Body.Bytes(), &list)
	if len(list.Recs) != 2 || list.Unread != 2 {
		t.Fatalf("list: %+v", list)
	}
	song, album := list.Recs[0], list.Recs[1]
	if song.Kind != "song" || song.Album != nil || song.Song.ID != "s1" {
		t.Fatalf("song rec: %+v", song)
	}
	if album.Kind != "album" || album.Album == nil || *album.Album != (RecAlbum{ID: "al1", Name: "Record", Artist: "Band", CoverArt: "c1"}) || album.Note != "whole thing" {
		t.Fatalf("album rec: %+v %+v", album, album.Album)
	}
}

func TestAlbumRecsShareRateLimit(t *testing.T) {
	a := newTestApp(t)
	s := a.store
	now := time.Now()
	s.TouchSeen("ann", now, "")
	for i := 0; i < recPerHour; i++ {
		if _, err := s.SendRecs("max", []string{"ann"}, RecSong{ID: "s1"}, "", now); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if _, err := s.SendAlbumRecs("max", []string{"ann"}, RecAlbum{ID: "al1"}, "", now); err != errRecLimit {
		t.Fatalf("want limit, got %v", err)
	}
}

func TestRecsKindMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE recommendations (id TEXT PRIMARY KEY, sender TEXT NOT NULL, recipient TEXT NOT NULL, song_id TEXT NOT NULL,
title TEXT NOT NULL DEFAULT '', artist TEXT NOT NULL DEFAULT '', album TEXT NOT NULL DEFAULT '', album_id TEXT NOT NULL DEFAULT '',
cover_art TEXT NOT NULL DEFAULT '', duration INTEGER NOT NULL DEFAULT 0, note TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
seen INTEGER NOT NULL DEFAULT 0, dismissed INTEGER NOT NULL DEFAULT 0);
INSERT INTO recommendations (id, sender, recipient, song_id, title, created_at) VALUES ('r1', 'max', 'ann', 's1', 'Old', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	list, err := s.Recs("ann", 10)
	if err != nil || len(list) != 1 || list[0].Kind != "song" || list[0].Song.Title != "Old" || list[0].Album != nil {
		t.Fatalf("old rec: %+v %v", list, err)
	}
}

func TestRecPushText(t *testing.T) {
	al := albumAsSong(RecAlbum{ID: "al1", Name: "Record", Artist: "Band"})
	if got := recPushText("en", "max", recKindAlbum, al); got != "max recommends the album Record – Band" {
		t.Fatalf("en: %q", got)
	}
	if got := recPushText("de", "max", recKindAlbum, al); got != "max empfiehlt das Album Record – Band" {
		t.Fatalf("de: %q", got)
	}
	if got := recPushText("en", "max", recKindSong, RecSong{Title: "Song", Artist: "Band"}); got != "max recommends Song – Band" {
		t.Fatalf("song: %q", got)
	}
}
