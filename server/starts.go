package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

const (
	startLogBytes  = 1024
	startKeepRows  = 2000
	startMaxMillis = 10 * 60 * 1000
	startRecent    = 30
	startGroupDays = 7
)

const startsSchema = `
CREATE TABLE IF NOT EXISTS start_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user TEXT NOT NULL,
	at INTEGER NOT NULL,
	device TEXT NOT NULL DEFAULT '',
	ms INTEGER NOT NULL,
	waits INTEGER NOT NULL DEFAULT 0,
	tier TEXT NOT NULL DEFAULT '',
	format TEXT NOT NULL DEFAULT '',
	kbps INTEGER NOT NULL DEFAULT 0,
	quality TEXT NOT NULL DEFAULT '',
	preload INTEGER NOT NULL DEFAULT 0,
	auto INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS start_events_at ON start_events(at);`

type StartEvent struct {
	User    string `json:"user"`
	At      int64  `json:"at"`
	Device  string `json:"device"`
	Ms      int64  `json:"ms"`
	Waits   int    `json:"waits"`
	Tier    string `json:"tier"`
	Format  string `json:"format"`
	Kbps    int    `json:"kbps"`
	Quality string `json:"quality"`
	Preload bool   `json:"preload"`
	Auto    bool   `json:"auto"`
}

type StartGroup struct {
	Device string `json:"device"`
	Tier   string `json:"tier"`
	Count  int    `json:"count"`
	Median int64  `json:"median"`
	Slow   int64  `json:"slow"`
}

type StartStats struct {
	Recent []StartEvent `json:"recent"`
	Groups []StartGroup `json:"groups"`
}

func (s *Store) RecordStart(e StartEvent) error {
	_, err := s.db.Exec(`INSERT INTO start_events (user, at, device, ms, waits, tier, format, kbps, quality, preload, auto) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.User, e.At, e.Device, e.Ms, e.Waits, e.Tier, e.Format, e.Kbps, e.Quality, e.Preload, e.Auto)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM start_events WHERE id <= (SELECT id FROM start_events ORDER BY id DESC LIMIT 1 OFFSET ?)`, startKeepRows)
	return err
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	if p == 0.5 && len(sorted)%2 == 0 {
		return (sorted[i] + sorted[i+1]) / 2
	}
	return sorted[i]
}

func (s *Store) StartStats(now time.Time) (*StartStats, error) {
	out := &StartStats{Recent: []StartEvent{}, Groups: []StartGroup{}}
	scan := func(r *sql.Rows) (StartEvent, error) {
		var e StartEvent
		err := r.Scan(&e.User, &e.At, &e.Device, &e.Ms, &e.Waits, &e.Tier, &e.Format, &e.Kbps, &e.Quality, &e.Preload, &e.Auto)
		return e, err
	}
	const cols = `user, at, device, ms, waits, tier, format, kbps, quality, preload, auto`
	rows, err := s.db.Query(`SELECT `+cols+` FROM start_events ORDER BY at DESC, id DESC LIMIT ?`, startRecent)
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		e, err := scan(r)
		if err == nil {
			out.Recent = append(out.Recent, e)
		}
		return err
	}); err != nil {
		return nil, err
	}
	type key struct{ device, tier string }
	groups := map[key][]int64{}
	rows, err = s.db.Query(`SELECT `+cols+` FROM start_events WHERE at >= ?`, now.AddDate(0, 0, -startGroupDays).Unix())
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		e, err := scan(r)
		if err != nil {
			return err
		}
		k := key{e.Device, e.Tier}
		groups[k] = append(groups[k], e.Ms)
		return nil
	}); err != nil {
		return nil, err
	}
	for k, ms := range groups {
		sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
		out.Groups = append(out.Groups, StartGroup{Device: k.device, Tier: k.tier, Count: len(ms), Median: percentile(ms, 0.5), Slow: percentile(ms, 0.9)})
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.Device != b.Device {
			return a.Device < b.Device
		}
		return a.Tier < b.Tier
	})
	return out, nil
}

func validTier(t string) string {
	switch t {
	case "original", "normal", "low":
		return t
	}
	return ""
}

func (a *App) handleStartLog(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Ms      float64 `json:"ms"`
		Waits   int     `json:"waits"`
		Tier    string  `json:"tier"`
		Format  string  `json:"format"`
		Kbps    int     `json:"kbps"`
		Quality string  `json:"quality"`
		Preload bool    `json:"preload"`
		Auto    bool    `json:"auto"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, startLogBytes)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	if body.Ms < 0 || body.Ms > startMaxMillis {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	now := time.Now()
	if !a.starts.allow(user, now) {
		writeError(w, http.StatusTooManyRequests, "rate-limited")
		return
	}
	e := StartEvent{
		User: clip(user, 64), At: now.Unix(), Device: deviceName(r.UserAgent()), Ms: int64(body.Ms),
		Waits: max(0, min(body.Waits, 999)), Tier: validTier(body.Tier), Format: clip(body.Format, 16),
		Kbps: max(0, min(body.Kbps, 100000)), Quality: clip(body.Quality, 16), Preload: body.Preload, Auto: body.Auto,
	}
	if err := a.store.RecordStart(e); err != nil {
		writeError(w, http.StatusInternalServerError, "stats")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
