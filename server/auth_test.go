package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginWrongPassword(t *testing.T) {
	a := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"max","password":"nope"}`))
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("cookie set on failed login")
	}
}

func TestUploadNeedsLogin(t *testing.T) {
	a := newTestApp(t)
	body, ct := multipartBody(t, formPart{"file", "a.flac", []byte("fLaC....")})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSessionCookieAndMe(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags: %+v", c)
	}
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(c)
	rec := do(t, a.Routes(), req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"max"`) {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("password leaked")
	}
	logout := httptest.NewRequest("POST", "/api/logout", nil)
	logout.AddCookie(c)
	do(t, a.Routes(), logout)
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(c)
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", rec.Code)
	}
}

func TestStoredCredsAreEncrypted(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	user, sealed, err := a.store.GetSession(hashSessionID(c.Value))
	if err != nil || user != "max" {
		t.Fatalf("session: %v %q", err, user)
	}
	if strings.Contains(string(sealed), `"t"`) {
		t.Fatal("token stored in plain text")
	}
	plain, err := a.open(sealed, hashSessionID(c.Value))
	if err != nil || !strings.Contains(string(plain), `"u":"max"`) {
		t.Fatalf("open: %v %s", err, plain)
	}
	if _, err := a.open(sealed, "other"); err == nil {
		t.Fatal("sealed creds opened with wrong aad")
	}
}

func TestCrossOriginUploadRejected(t *testing.T) {
	a := newTestApp(t)
	body, ct := multipartBody(t, formPart{"file", "a.flac", []byte("fLaC....")})
	req := httptest.NewRequest("POST", "/api/uploads", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Origin", "http://evil.example")
	rec := authed(t, a, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	a := newTestApp(t)
	var last int
	for i := 0; i < loginAttempts+1; i++ {
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"max","password":"nope"}`))
		last = do(t, a.Routes(), req).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last status %d", last)
	}
}

func loginRemember(t *testing.T, a *App, remember string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"max","password":"secret","remember":`+remember+`}`))
	rec := do(t, a.Routes(), req)
	if rec.Code != 200 {
		t.Fatalf("login status %d: %s", rec.Code, rec.Body)
	}
	return rec
}

func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestRememberLogin(t *testing.T) {
	a := newTestApp(t)
	long := loginRemember(t, a, "true")
	short := loginRemember(t, a, "false")
	lc, sc := sessionCookieOf(t, long), sessionCookieOf(t, short)
	if lc.MaxAge != int(sessionTTL.Seconds()) {
		t.Fatalf("remembered cookie MaxAge %d", lc.MaxAge)
	}
	if sc.MaxAge != 0 || !sc.Expires.IsZero() || strings.Contains(short.Header().Get("Set-Cookie"), "Max-Age") {
		t.Fatalf("session cookie should not persist: %q", short.Header().Get("Set-Cookie"))
	}
	if !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags: %+v", sc)
	}
	le, err := a.store.SessionExpiry(hashSessionID(lc.Value))
	if err != nil {
		t.Fatal(err)
	}
	se, err := a.store.SessionExpiry(hashSessionID(sc.Value))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(le); d < sessionTTL-time.Minute || d > sessionTTL+time.Minute {
		t.Fatalf("remembered expiry in %v", d)
	}
	if d := time.Until(se); d < shortTTL-time.Minute || d > shortTTL+time.Minute {
		t.Fatalf("short expiry in %v", d)
	}
	for _, rec := range []*httptest.ResponseRecorder{long, short} {
		all := rec.Body.String() + strings.Join(rec.Header().Values("Set-Cookie"), ";")
		if strings.Contains(all, "secret") {
			t.Fatal("password leaked into response")
		}
	}
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(sc)
	if rec := do(t, a.Routes(), req); rec.Code != 200 {
		t.Fatalf("short session me: %d", rec.Code)
	}
}

func TestRememberDefaultsToTrue(t *testing.T) {
	a := newTestApp(t)
	if c := login(t, a); c.MaxAge != int(sessionTTL.Seconds()) {
		t.Fatalf("default MaxAge %d", c.MaxAge)
	}
}
