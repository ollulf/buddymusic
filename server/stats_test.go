package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const iphoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func TestDeviceName(t *testing.T) {
	cases := map[string]string{
		iphoneUA: "iPhone · Safari",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36": "Windows · Chrome",
		"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0":                                      "Linux · Firefox",
		"curl/8.0": "Other",
		"":         "",
	}
	for ua, want := range cases {
		if got := deviceName(ua); got != want {
			t.Errorf("%q: got %q want %q", ua, got, want)
		}
	}
}

func TestStoreStatsDashboard(t *testing.T) {
	a := newTestApp(t)
	s := a.store
	now := time.Now()
	s.RecordLogin("max", now.Add(-time.Hour), true, "iPhone · Safari", true)
	s.RecordLogin("max", now.Add(-time.Minute), false, "Mac · Firefox", false)
	s.TouchSeen("max", now, "iPhone · Safari")
	s.AddStreamBytes("max", "t1", "iPhone · Safari", now.Add(-time.Minute), 1000, false)
	s.AddStreamBytes("max", "t1", "iPhone · Safari", now, 500, true)
	if err := s.RecordProgress("max", "t1", "One", "Blue", now, 120, true); err != nil {
		t.Fatal(err)
	}
	s.RecordProgress("max", "t1", "", "", now, 60, true)
	if err := s.RecordProgress("max", "nope", "", "", now, 5, false); err != ErrNotFound {
		t.Fatalf("unknown play: %v", err)
	}
	s.AddStreamBytes("ann", "t2", "Android · Chrome", now.Add(-48*time.Hour), 300, false)
	d, err := s.Dashboard(now, 7)
	if err != nil {
		t.Fatal(err)
	}
	if d.Totals.Plays != 2 || d.Totals.Bytes != 1800 || d.Totals.Seconds != 120 || d.Totals.Logins != 1 || d.Totals.Failed != 1 || d.Totals.Active != 2 {
		t.Fatalf("totals: %+v", d.Totals)
	}
	if len(d.Users) != 2 || d.Users[0].User != "max" {
		t.Fatalf("users: %+v", d.Users)
	}
	max := d.Users[0]
	if max.Plays != 1 || max.Bytes != 1500 || max.Daily[6] != 120 || max.LastSeen != now.Unix() || len(max.Devices) != 1 {
		t.Fatalf("max: %+v", max)
	}
	if len(d.NowPlaying) != 1 || d.NowPlaying[0].Title != "One" || !d.NowPlaying[0].Saver {
		t.Fatalf("now playing: %+v", d.NowPlaying)
	}
	if len(d.Logins) != 2 || d.Logins[0].OK {
		t.Fatalf("logins: %+v", d.Logins)
	}
}

func TestPurgeStats(t *testing.T) {
	a := newTestApp(t)
	s := a.store
	now := time.Now()
	old := now.Add(-statsRetention - time.Hour)
	s.RecordLogin("max", old, true, "", false)
	s.AddStreamBytes("max", "t1", "", old, 10, false)
	s.TouchSeen("max", old, "")
	s.RecordLogin("ann", now, true, "", false)
	if err := s.PurgeStats(now.Add(-statsRetention)); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM login_events) + (SELECT COUNT(*) FROM play_events) + (SELECT COUNT(*) FROM user_seen)`).Scan(&n)
	if n != 1 {
		t.Fatalf("rows left: %d", n)
	}
}

func TestLoginEventsRecorded(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"Max","password":"nope"}`))
	req.Header.Set("User-Agent", iphoneUA)
	do(t, a.Routes(), req)
	login(t, a)
	d, err := a.store.Dashboard(time.Now(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Logins) != 2 || d.Logins[1].OK || d.Logins[1].User != "max" || d.Logins[1].Device != "iPhone · Safari" || !d.Logins[0].OK {
		t.Fatalf("logins: %+v", d.Logins)
	}
	if strings.Contains(d.Logins[1].Device, "nope") {
		t.Fatal("password stored")
	}
}

func TestStreamAndProgressRecorded(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	req := httptest.NewRequest("GET", "/api/music/stream/s1?format=opus&maxBitRate=128", nil)
	req.AddCookie(c)
	if rec := do(t, a.Routes(), req); rec.Code != 200 {
		t.Fatalf("stream %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/music/progress/s1", strings.NewReader(`{"seconds":42.5,"playing":true,"title":"One","artist":"Blue"}`))
	req.AddCookie(c)
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusNoContent {
		t.Fatalf("progress %d %s", rec.Code, rec.Body)
	}
	d, _ := a.store.Dashboard(time.Now(), 7)
	if d.Totals.Plays != 1 || d.Totals.Bytes != int64(len("0123456789opus128")) || d.Totals.Seconds != 42 {
		t.Fatalf("totals: %+v", d.Totals)
	}
	if len(d.Users) != 1 || d.Users[0].LastSeen == 0 {
		t.Fatalf("users: %+v", d.Users)
	}
}

func TestAdminStatsAdminOnly(t *testing.T) {
	a := newTestApp(t)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/admin/stats", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", rec.Code)
	}
	if rec := do(t, a.Routes(), httptest.NewRequest("GET", "/api/admin/stats", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rec.Code)
	}
	rec = adminReq(t, a, "GET", "/api/admin/stats")
	if rec.Code != 200 {
		t.Fatalf("admin: %d %s", rec.Code, rec.Body)
	}
	var d Dashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || len(d.Days) != 7 || len(d.Logins) != 2 {
		t.Fatalf("body: %v %s", err, rec.Body)
	}
}
