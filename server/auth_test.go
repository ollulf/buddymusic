package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
