package main

import (
	"database/sql"
	"net/http"
	"sort"
	"time"
)

const (
	friendOnlineGap = 5 * time.Minute
	friendRecentGap = time.Hour
)

const friendsSchema = `
CREATE TABLE IF NOT EXISTS user_prefs (
	user TEXT PRIMARY KEY,
	share_listening INTEGER NOT NULL DEFAULT 1
);`

type Friend struct {
	User     string `json:"user"`
	Online   bool   `json:"online"`
	LastSeen int64  `json:"lastSeen"`
	State    string `json:"state"`
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Avatar   int64  `json:"avatar"`
}

func (s *Store) Friends(self string, now time.Time) ([]Friend, error) {
	out := []Friend{}
	rows, err := s.db.Query(`SELECT s.user, s.last_seen, COALESCE(a.updated_at, 0)
FROM user_seen s LEFT JOIN avatars a ON a.user = s.user
WHERE s.user != ? AND s.last_seen >= ?`, self, now.Add(-friendRecentGap).Unix())
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var f Friend
		if err := r.Scan(&f.User, &f.LastSeen, &f.Avatar); err != nil {
			return err
		}
		f.Online = f.LastSeen >= now.Add(-friendOnlineGap).Unix()
		f.State = "idle"
		out = append(out, f)
		return nil
	}); err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, f := range out {
		idx[f.User] = i
	}
	done := map[string]bool{}
	rows, err = s.db.Query(`SELECT user, title, artist, playing, updated_at FROM play_events WHERE user != ? AND updated_at >= ? ORDER BY updated_at DESC`,
		self, now.Add(-nowPlayingGap).Unix())
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var u, title, artist string
		var playing bool
		var at int64
		if err := r.Scan(&u, &title, &artist, &playing, &at); err != nil {
			return err
		}
		i, ok := idx[u]
		if !ok || done[u] {
			return nil
		}
		done[u] = true
		if at > out[i].LastSeen {
			out[i].LastSeen = at
		}
		out[i].Online = true
		if title == "" {
			return nil
		}
		out[i].Title, out[i].Artist = title, artist
		out[i].State = "paused"
		if playing {
			out[i].State = "playing"
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].LastSeen != out[j].LastSeen {
			return out[i].LastSeen > out[j].LastSeen
		}
		return out[i].User < out[j].User
	})
	return out, nil
}

func (a *App) handleFriends(w http.ResponseWriter, r *http.Request, user string) {
	list, err := a.store.Friends(user, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"friends": list})
}

func (a *App) handleFriendAvatar(w http.ResponseWriter, r *http.Request, user string) {
	a.handleGetAvatar(w, r, r.PathValue("user"))
}
