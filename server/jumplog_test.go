package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestJumpLog(t *testing.T) {
	a := newTestApp(t)
	body := `{"track":"s1","format":"opus","kbps":128,"from":61.2,"to":59.9,"expected":62.1,"gap":-2.2,"visibility":"hidden","agent":"ios-safari","event":"advance-refused","detail":"NotAllowedError"}`
	if rec := do(t, a.Routes(), httptest.NewRequest("POST", "/api/music/jumplog", strings.NewReader(body))); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", rec.Code)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	c := login(t, a)
	post := func(b string) int {
		req := httptest.NewRequest("POST", "/api/music/jumplog", strings.NewReader(b))
		req.AddCookie(c)
		return do(t, a.Routes(), req).Code
	}
	if code := post(body); code != http.StatusNoContent {
		t.Fatalf("authed: %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "jumplog") || !strings.Contains(out, `track="s1"`) || !strings.Contains(out, `vis="hidden"`) || !strings.Contains(out, `event="advance-refused"`) || !strings.Contains(out, `detail="NotAllowedError"`) {
		t.Fatalf("log: %s", out)
	}
	if code := post(`{"track":"` + strings.Repeat("x", jumpLogBytes) + `"}`); code != http.StatusBadRequest {
		t.Fatalf("oversize: %d", code)
	}
	if code := post(`not json`); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
	for i := 1; i < jumpLogMax; i++ {
		if code := post(body); code != http.StatusNoContent {
			t.Fatalf("report %d: %d", i, code)
		}
	}
	if code := post(body); code != http.StatusTooManyRequests {
		t.Fatalf("rate limit: %d", code)
	}
}
