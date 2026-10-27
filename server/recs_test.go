package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
