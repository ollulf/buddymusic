package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStoreFriends(t *testing.T) {
	a := newTestApp(t)
	s := a.store
	now := time.Now()
	s.TouchSeen("max", now, "iPhone · Safari")
	s.TouchSeen("ann", now.Add(-time.Minute), "Android · Chrome")
	s.TouchSeen("bob", now.Add(-20*time.Minute), "Mac · Firefox")
	s.TouchSeen("old", now.Add(-3*time.Hour), "Mac · Firefox")
	s.TouchSeen("cat", now, "Windows · Edge")
	s.AddStreamBytes("ann", "t1", "Android · Chrome", now, 100, false)
	s.RecordProgress("ann", "t1", "One", "Blue", now, 30, true)
	s.AddStreamBytes("cat", "t2", "Windows · Edge", now, 100, false)
	s.RecordProgress("cat", "t2", "Two", "Red", now, 30, false)
	s.AddStreamBytes("max", "t3", "iPhone · Safari", now, 100, false)
	s.RecordProgress("max", "t3", "Mine", "Me", now, 30, true)

	list, err := s.Friends("max", now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Friend{}
	for _, f := range list {
		got[f.User] = f
	}
	if len(list) != 3 || got["max"].User != "" || got["old"].User != "" {
		t.Fatalf("list: %+v", list)
	}
	if f := got["ann"]; !f.Online || f.State != "playing" || f.Title != "One" || f.Artist != "Blue" {
		t.Fatalf("ann: %+v", f)
	}
	if f := got["cat"]; !f.Online || f.State != "paused" || f.Title != "Two" {
		t.Fatalf("cat: %+v", f)
	}
	if f := got["bob"]; f.Online || f.State != "idle" || f.LastSeen != now.Add(-20*time.Minute).Unix() {
		t.Fatalf("bob: %+v", f)
	}
	if list[2].User != "bob" {
		t.Fatalf("offline friends last: %+v", list)
	}

}

func TestFriendsHandlers(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	get := func(c *http.Cookie, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(c)
		return do(t, a.Routes(), req)
	}
	get(max, "/api/me")
	get(boss, "/api/me")
	a.store.AddStreamBytes("boss", "t1", "Mac · Firefox", time.Now(), 100, false)
	a.store.RecordProgress("boss", "t1", "Song", "Band", time.Now(), 10, true)

	rec := get(max, "/api/friends")
	if rec.Code != 200 {
		t.Fatalf("friends %d", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"device", "Mac", "bytes", "seconds", "login", "saver"} {
		if strings.Contains(body, leak) {
			t.Fatalf("leaked %q: %s", leak, body)
		}
	}
	var out struct {
		Friends []Friend
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Friends) != 1 || out.Friends[0].User != "boss" || out.Friends[0].Title != "Song" {
		t.Fatalf("out: %s", body)
	}

	req := httptest.NewRequest("PUT", "/api/friends/share", strings.NewReader(`{"share":false}`))
	req.AddCookie(boss)
	if rec := do(t, a.Routes(), req); rec.Code == 200 {
		t.Fatal("share toggle should be gone")
	}

	req = httptest.NewRequest("GET", "/api/friends", nil)
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon %d", rec.Code)
	}
}

func TestFriendAvatar(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	img := pngBytes(t, 16, 16)
	if rec := avatarReq(t, a, boss, "PUT", img); rec.Code != 200 {
		t.Fatalf("put %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/avatar/boss", nil)
	req.AddCookie(max)
	rec := do(t, a.Routes(), req)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), img) || rec.Header().Get("ETag") == "" {
		t.Fatalf("friend avatar %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/avatar/boss", nil)
	req.AddCookie(max)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusNotModified {
		t.Fatalf("etag %d", rec.Code)
	}
	for _, m := range []string{"PUT", "DELETE"} {
		req = httptest.NewRequest(m, "/api/avatar/boss", bytes.NewReader(img))
		req.AddCookie(max)
		if rec := do(t, a.Routes(), req); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s other avatar %d", m, rec.Code)
		}
	}
	if rec := avatarReq(t, a, boss, "GET", nil); rec.Code != 200 {
		t.Fatalf("boss avatar gone %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/avatar/boss", nil)
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon %d", rec.Code)
	}
}
