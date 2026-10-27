package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	recNoteMax  = 140
	recPerHour  = 20
	recMaxTo    = 10
	recList     = 50
	recFieldMax = 300
)

const recsSchema = `
CREATE TABLE IF NOT EXISTS recommendations (
	id TEXT PRIMARY KEY,
	sender TEXT NOT NULL,
	recipient TEXT NOT NULL,
	song_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	artist TEXT NOT NULL DEFAULT '',
	album TEXT NOT NULL DEFAULT '',
	album_id TEXT NOT NULL DEFAULT '',
	cover_art TEXT NOT NULL DEFAULT '',
	duration INTEGER NOT NULL DEFAULT 0,
	note TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	seen INTEGER NOT NULL DEFAULT 0,
	dismissed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS recs_recipient ON recommendations(recipient, created_at);
CREATE INDEX IF NOT EXISTS recs_sender ON recommendations(sender, created_at);`

type RecSong struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  string `json:"albumId"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"`
}

type Rec struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	Avatar    int64   `json:"avatar"`
	Song      RecSong `json:"song"`
	Note      string  `json:"note"`
	CreatedAt int64   `json:"createdAt"`
	Seen      bool    `json:"seen"`
}

var (
	errRecSelf    = errors.New("self")
	errRecUnknown = errors.New("unknown-user")
	errRecLimit   = errors.New("rate-limited")
)

func (s *Store) userKnown(user string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM user_seen WHERE user = ?`, user).Scan(&n)
	return n > 0
}

func (s *Store) SendRecs(from string, to []string, song RecSong, note string, now time.Time) (int, error) {
	seen := map[string]bool{}
	list := []string{}
	for _, u := range to {
		if u == from {
			return 0, errRecSelf
		}
		if seen[u] {
			continue
		}
		if !s.userKnown(u) {
			return 0, errRecUnknown
		}
		seen[u] = true
		list = append(list, u)
	}
	var sent int
	s.db.QueryRow(`SELECT COUNT(*) FROM recommendations WHERE sender = ? AND created_at > ?`, from, now.Add(-time.Hour).Unix()).Scan(&sent)
	if sent+len(list) > recPerHour {
		return 0, errRecLimit
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, u := range list {
		if _, err := tx.Exec(`INSERT INTO recommendations (id, sender, recipient, song_id, title, artist, album, album_id, cover_art, duration, note, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, newID(), from, u, song.ID, song.Title, song.Artist, song.Album, song.AlbumID, song.CoverArt, song.Duration, note, now.Unix()); err != nil {
			return 0, err
		}
	}
	return len(list), tx.Commit()
}

func (s *Store) Recs(user string, limit int) ([]Rec, error) {
	out := []Rec{}
	rows, err := s.db.Query(`SELECT r.id, r.sender, COALESCE(a.updated_at, 0), r.song_id, r.title, r.artist, r.album, r.album_id, r.cover_art, r.duration, r.note, r.created_at, r.seen
FROM recommendations r LEFT JOIN avatars a ON a.user = r.sender
WHERE r.recipient = ? AND r.dismissed = 0 ORDER BY r.created_at DESC, r.rowid DESC LIMIT ?`, user, limit)
	err = scanEach(rows, err, func(r *sql.Rows) error {
		var x Rec
		if err := r.Scan(&x.ID, &x.From, &x.Avatar, &x.Song.ID, &x.Song.Title, &x.Song.Artist, &x.Song.Album, &x.Song.AlbumID, &x.Song.CoverArt, &x.Song.Duration, &x.Note, &x.CreatedAt, &x.Seen); err != nil {
			return err
		}
		out = append(out, x)
		return nil
	})
	return out, err
}

func (s *Store) UnreadRecs(user string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM recommendations WHERE recipient = ? AND seen = 0 AND dismissed = 0`, user).Scan(&n)
	return n
}

func (s *Store) MarkRecs(user, id string, seen, dismiss bool) error {
	q := `UPDATE recommendations SET seen = MAX(seen, ?), dismissed = MAX(dismissed, ?) WHERE recipient = ?`
	args := []any{seen || dismiss, dismiss, user}
	if id != "all" {
		q += ` AND id = ?`
		args = append(args, id)
	}
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 && id != "all" {
		return ErrNotFound
	}
	return nil
}

func clipField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > recFieldMax {
		s = s[:recFieldMax]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func (a *App) handleSendRec(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Song RecSong  `json:"song"`
		To   []string `json:"to"`
		Note string   `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	note := strings.TrimSpace(body.Note)
	if !validID(body.Song.ID) || len(body.To) == 0 || len(body.To) > recMaxTo || utf8.RuneCountInString(note) > recNoteMax {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	song := RecSong{ID: body.Song.ID, Title: clipField(body.Song.Title), Artist: clipField(body.Song.Artist), Album: clipField(body.Song.Album),
		AlbumID: clipField(body.Song.AlbumID), CoverArt: clipField(body.Song.CoverArt), Duration: max(0, body.Song.Duration)}
	n, err := a.store.SendRecs(user, body.To, song, note, time.Now())
	switch {
	case errors.Is(err, errRecSelf), errors.Is(err, errRecUnknown):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errRecLimit):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "store")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"sent": n})
	}
}

func (a *App) handleListRecs(w http.ResponseWriter, r *http.Request, user string) {
	list, err := a.store.Recs(user, recList)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recs": list, "unread": a.store.UnreadRecs(user)})
}

func (a *App) handleMarkRec(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Seen    bool `json:"seen"`
		Dismiss bool `json:"dismiss"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if id != "all" && !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	err := a.store.MarkRecs(user, id, body.Seen, body.Dismiss)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not-found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "store")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"unread": a.store.UnreadRecs(user)})
	}
}
