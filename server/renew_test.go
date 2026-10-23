package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

type meRenewed struct {
	OpenedAlbums  []string         `json:"openedAlbums"`
	RenewedAlbums map[string]int64 `json:"renewedAlbums"`
}

func TestAlbumRenewEndpoint(t *testing.T) {
	a := newTestApp(t)
	rec := authed(t, a, httptest.NewRequest("GET", "/api/me", nil))
	var me meRenewed
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.RenewedAlbums == nil || len(me.RenewedAlbums) != 0 {
		t.Fatalf("renewedAlbums should be an empty map: %s", rec.Body)
	}
	if rec = authed(t, a, httptest.NewRequest("POST", "/api/admin/album/al-1/renew", nil)); rec.Code != 403 {
		t.Fatalf("non-admin renew: %d", rec.Code)
	}
	if got := a.store.RenewedAlbums(); len(got) != 0 {
		t.Fatalf("non-admin renew stored: %v", got)
	}
	a.store.MarkAlbumOpened("someone", "al-1", time.Now())
	a.store.MarkAlbumOpened("someone", "al-2", time.Now())
	if rec = authed(t, a, httptest.NewRequest("POST", "/api/music/album/al-1/opened", nil)); rec.Code != 204 {
		t.Fatalf("mark opened: %d", rec.Code)
	}
	before := time.Now().Unix()
	if rec = adminReq(t, a, "POST", "/api/admin/album/al-1/renew"); rec.Code != 204 {
		t.Fatalf("admin renew: %d %s", rec.Code, rec.Body)
	}
	if got := a.store.OpenedAlbums("someone"); len(got) != 1 || got[0] != "al-2" {
		t.Fatalf("opened rows after renew: %v", got)
	}
	rec = authed(t, a, httptest.NewRequest("GET", "/api/me", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if len(me.OpenedAlbums) != 0 {
		t.Fatalf("own opened rows kept: %v", me.OpenedAlbums)
	}
	if at := me.RenewedAlbums["al-1"]; at < before || len(me.RenewedAlbums) != 1 {
		t.Fatalf("renewedAlbums: %v", me.RenewedAlbums)
	}
}
