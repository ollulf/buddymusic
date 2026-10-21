package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartLog(t *testing.T) {
	a := newTestApp(t)
	body := `{"ms":2400,"waits":1,"tier":"original","format":"","kbps":1000,"quality":"auto","preload":false,"auto":false}`
	if rec := do(t, a.Routes(), httptest.NewRequest("POST", "/api/music/startlog", strings.NewReader(body))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rec.Code)
	}
	c := login(t, a)
	post := func(b string) int {
		req := httptest.NewRequest("POST", "/api/music/startlog", strings.NewReader(b))
		req.Header.Set("User-Agent", iphoneUA)
		req.AddCookie(c)
		return do(t, a.Routes(), req).Code
	}
	for _, b := range []string{body, `{"ms":600,"tier":"original"}`, `{"ms":300,"tier":"normal","format":"opus","kbps":128,"preload":true}`} {
		if code := post(b); code != http.StatusNoContent {
			t.Fatalf("post %s: %d", b, code)
		}
	}
	if code := post(`{"ms":-5}`); code != http.StatusBadRequest {
		t.Fatalf("negative: %d", code)
	}
	if code := post(`nope`); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
	st, err := a.store.StartStats(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Recent) != 3 || st.Recent[0].Device != "iPhone · Safari" {
		t.Fatalf("recent: %+v", st.Recent)
	}
	if len(st.Groups) != 2 {
		t.Fatalf("groups: %+v", st.Groups)
	}
	g := st.Groups[1]
	if g.Tier != "original" || g.Count != 2 || g.Median != 1500 {
		t.Fatalf("original group: %+v", g)
	}
	if !st.Recent[0].Preload || st.Recent[0].Format != "opus" {
		t.Fatalf("latest: %+v", st.Recent[0])
	}
	if err := a.store.PurgeStats(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.store.StartStats(time.Now()); len(st.Recent) != 0 {
		t.Fatalf("purge kept %d", len(st.Recent))
	}
}

func TestStartLogCap(t *testing.T) {
	a := newTestApp(t)
	for i := 0; i < startKeepRows+5; i++ {
		if err := a.store.RecordStart(StartEvent{User: "u", At: int64(i), Ms: 100}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := a.store.db.QueryRow(`SELECT COUNT(*) FROM start_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != startKeepRows {
		t.Fatalf("rows: %d", n)
	}
}
