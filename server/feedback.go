package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	feedbackBytes   = 8 << 10
	feedbackMaxText = 2000
	feedbackList    = 50
)

const feedbackSchema = `
CREATE TABLE IF NOT EXISTS feedback (
	id TEXT PRIMARY KEY,
	created_at INTEGER NOT NULL,
	user TEXT NOT NULL,
	lang TEXT NOT NULL DEFAULT '',
	page TEXT NOT NULL DEFAULT '',
	version TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL,
	text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS feedback_created ON feedback(created_at);`

type Feedback struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	User      string `json:"user"`
	Lang      string `json:"lang"`
	Page      string `json:"page"`
	Version   string `json:"version"`
	UserAgent string `json:"userAgent"`
	Category  string `json:"category"`
	Text      string `json:"text"`
}

func validCategory(c string) bool {
	switch c {
	case "bug", "idea", "other":
		return true
	}
	return false
}

func (s *Store) SaveFeedback(f Feedback) error {
	_, err := s.db.Exec(`INSERT INTO feedback (id, created_at, user, lang, page, version, user_agent, category, text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.CreatedAt, f.User, f.Lang, f.Page, f.Version, f.UserAgent, f.Category, f.Text)
	return err
}

func (s *Store) RecentFeedback(limit int) ([]Feedback, error) {
	out := []Feedback{}
	rows, err := s.db.Query(`SELECT id, created_at, user, lang, page, version, user_agent, category, text FROM feedback ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	err = scanEach(rows, err, func(r *sql.Rows) error {
		var f Feedback
		if err := r.Scan(&f.ID, &f.CreatedAt, &f.User, &f.Lang, &f.Page, &f.Version, &f.UserAgent, &f.Category, &f.Text); err != nil {
			return err
		}
		out = append(out, f)
		return nil
	})
	return out, err
}

func feedbackFileName(f Feedback) string {
	short := f.ID
	if len(short) > 8 {
		short = short[:8]
	}
	return strconv.FormatInt(f.CreatedAt, 10) + "-" + short + ".json"
}

func writeFeedbackFile(dir string, f Feedback) error {
	if err := os.MkdirAll(dir, 0o2775); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".feedback-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, feedbackFileName(f)))
}

func (a *App) handleFeedback(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Category string `json:"category"`
		Text     string `json:"text"`
		Lang     string `json:"lang"`
		Page     string `json:"page"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, feedbackBytes)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > feedbackMaxText {
		writeError(w, http.StatusBadRequest, "bad-text")
		return
	}
	if !validCategory(body.Category) {
		writeError(w, http.StatusBadRequest, "bad-category")
		return
	}
	now := time.Now()
	if !a.feedback.allow(user, now) {
		writeError(w, http.StatusTooManyRequests, "rate-limited")
		return
	}
	f := Feedback{
		ID:        newID(),
		CreatedAt: now.UnixMilli(),
		User:      clip(user, 64),
		Lang:      clip(body.Lang, 8),
		Page:      clip(body.Page, 200),
		Version:   version,
		UserAgent: deviceName(r.UserAgent()),
		Category:  body.Category,
		Text:      text,
	}
	if err := writeFeedbackFile(a.cfg.FeedbackDir(), f); err != nil {
		log.Printf("feedback file: %v", err)
		writeError(w, http.StatusInternalServerError, "save-failed")
		return
	}
	if err := a.store.SaveFeedback(f); err != nil {
		log.Printf("feedback db: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAdminFeedback(w http.ResponseWriter, r *http.Request, user string) {
	if !a.cachedAdmin(r) {
		writeError(w, http.StatusForbidden, "admin-only")
		return
	}
	list, err := a.store.RecentFeedback(feedbackList)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "feedback")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedback": list})
}
