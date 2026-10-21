package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postFeedback(t *testing.T, a *App, c *http.Cookie, body string, hdr map[string]string) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(body))
	req.Header.Set("User-Agent", iphoneUA)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if c != nil {
		req.AddCookie(c)
	}
	return do(t, a.Routes(), req).Code
}

func TestFeedbackSaved(t *testing.T) {
	a := newTestApp(t)
	if code := postFeedback(t, a, nil, `{"category":"bug","text":"hi"}`, nil); code != http.StatusUnauthorized {
		t.Fatalf("anon: %d", code)
	}
	c := login(t, a)
	if code := postFeedback(t, a, c, `{"category":"idea","text":"  More shuffle please  ","lang":"de","page":"#/library"}`, nil); code != http.StatusNoContent {
		t.Fatalf("post: %d", code)
	}
	dir := a.cfg.FeedbackDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".json") || strings.HasPrefix(entries[0].Name(), ".") {
		t.Fatalf("files: %v", entries)
	}
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode: %v", info.Mode())
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var f Feedback
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Text != "More shuffle please" || f.Category != "idea" || f.User != "max" || f.Lang != "de" || f.Page != "#/library" || f.UserAgent != "iPhone · Safari" || f.ID == "" || f.CreatedAt == 0 || f.Version == "" {
		t.Fatalf("file: %+v", f)
	}
	if entries[0].Name() != feedbackFileName(f) {
		t.Fatalf("name %s", entries[0].Name())
	}
	list, err := a.store.RecentFeedback(10)
	if err != nil || len(list) != 1 || list[0] != f {
		t.Fatalf("db: %+v %v", list, err)
	}
}

func TestFeedbackRejects(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	cases := map[string]string{
		"empty":    `{"category":"bug","text":"   "}`,
		"long":     `{"category":"bug","text":"` + strings.Repeat("x", feedbackMaxText+1) + `"}`,
		"category": `{"category":"rant","text":"hi"}`,
		"json":     `nope`,
	}
	for name, body := range cases {
		if code := postFeedback(t, a, c, body, nil); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", name, code)
		}
	}
	if code := postFeedback(t, a, c, `{"category":"bug","text":"hi"}`, map[string]string{"Origin": "https://evil.example"}); code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", code)
	}
	if code := postFeedback(t, a, c, `{"category":"other","text":"`+strings.Repeat("ü", feedbackMaxText)+`"}`, nil); code != http.StatusNoContent {
		t.Fatalf("max length: %d", code)
	}
	if entries, _ := os.ReadDir(a.cfg.FeedbackDir()); len(entries) != 1 {
		t.Fatalf("files: %d", len(entries))
	}
}

func TestFeedbackRateLimit(t *testing.T) {
	a := newTestApp(t)
	c := login(t, a)
	for i := 0; i < 5; i++ {
		if code := postFeedback(t, a, c, `{"category":"bug","text":"hi"}`, nil); code != http.StatusNoContent {
			t.Fatalf("post %d: %d", i, code)
		}
	}
	if code := postFeedback(t, a, c, `{"category":"bug","text":"hi"}`, nil); code != http.StatusTooManyRequests {
		t.Fatalf("limit: %d", code)
	}
}

func TestAdminFeedback(t *testing.T) {
	a := newTestApp(t)
	if code := postFeedback(t, a, login(t, a), `{"category":"bug","text":"broken"}`, nil); code != http.StatusNoContent {
		t.Fatalf("post: %d", code)
	}
	req := httptest.NewRequest("GET", "/api/admin/feedback", nil)
	req.AddCookie(login(t, a))
	if rec := do(t, a.Routes(), req); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/admin/feedback", nil)
	req.AddCookie(loginAs(t, a, "boss", "bosspw"))
	rec := do(t, a.Routes(), req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: %d", rec.Code)
	}
	var out struct {
		Feedback []Feedback `json:"feedback"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Feedback) != 1 || out.Feedback[0].Text != "broken" {
		t.Fatalf("list: %s", rec.Body)
	}
}
