package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchSeenPrevVisit(t *testing.T) {
	s := newTestApp(t).store
	start := time.Unix(1_800_000_000, 0)
	if got := s.NewSince("max", start); got != start.Unix() {
		t.Fatalf("unknown user: got %d want now", got)
	}
	s.TouchSeen("max", start, "")
	if got := s.NewSince("max", start.Add(time.Hour)); got != start.Unix() {
		t.Fatalf("first visit: got %d want %d", got, start.Unix())
	}
	s.TouchSeen("max", start.Add(10*time.Minute), "")
	s.TouchSeen("max", start.Add(20*time.Minute), "")
	if got := s.NewSince("max", start); got != start.Unix() {
		t.Fatalf("same visit moved newSince: %d", got)
	}
	later := start.Add(20*time.Minute + 2*time.Hour)
	s.TouchSeen("max", later, "")
	if got := s.NewSince("max", later); got != start.Add(20*time.Minute).Unix() {
		t.Fatalf("new visit: got %d want end of previous visit", got)
	}
	s.TouchSeen("max", later.Add(5*time.Minute), "")
	if got := s.NewSince("max", later); got != start.Add(20*time.Minute).Unix() {
		t.Fatalf("newSince moved within a visit: %d", got)
	}
}

func TestMigrateUserSeen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE user_seen (user TEXT PRIMARY KEY, last_seen INTEGER NOT NULL, device TEXT NOT NULL DEFAULT '');
INSERT INTO user_seen VALUES ('max', 1700000000, '')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.NewSince("max", time.Now()); got != 1700000000 {
		t.Fatalf("migrated prev_seen: got %d", got)
	}
}

type meBody struct {
	User      string   `json:"user"`
	NewSince  int64    `json:"newSince"`
	OwnAlbums []string `json:"ownAlbums"`
}

func getMe(t *testing.T, a *App) meBody {
	t.Helper()
	rec := authed(t, a, httptest.NewRequest("GET", "/api/me", nil))
	var me meBody
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	return me
}

func TestMeNewSinceAndOwnAlbums(t *testing.T) {
	a := newTestApp(t)
	before := time.Now().Unix()
	me := getMe(t, a)
	if me.NewSince < before-1 || me.NewSince > time.Now().Unix() {
		t.Fatalf("newSince on first visit: %d", me.NewSince)
	}
	if me.OwnAlbums == nil || len(me.OwnAlbums) != 0 {
		t.Fatalf("ownAlbums should be an empty list: %v", me.OwnAlbums)
	}
	a.store.SaveUpload(&Upload{ID: "u1", User: me.User, Filename: "a.mp3", Status: "in-library", Artist: "Blue", Album: "Sky ", CreatedAt: 1})
	a.store.SaveUpload(&Upload{ID: "u2", User: me.User, Filename: "b.mp3", Status: "rejected", Artist: "Red", Album: "Sea", CreatedAt: 1})
	a.store.SaveUpload(&Upload{ID: "u3", User: "someone-else", Filename: "c.mp3", Status: "in-library", Artist: "Ann", Album: "Hers", CreatedAt: 1})
	me = getMe(t, a)
	if len(me.OwnAlbums) != 1 || me.OwnAlbums[0] != "blue|sky" {
		t.Fatalf("ownAlbums: %v", me.OwnAlbums)
	}
}
