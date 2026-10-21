package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	statsRetention = 90 * 24 * time.Hour
	seenEvery      = time.Minute
	playMergeGap   = 10 * time.Minute
	nowPlayingGap  = 90 * time.Second
	maxPlaySeconds = 6 * 3600
)

const statsSchema = `
CREATE TABLE IF NOT EXISTS login_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user TEXT NOT NULL,
	at INTEGER NOT NULL,
	ok INTEGER NOT NULL,
	device TEXT NOT NULL DEFAULT '',
	remember INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS login_events_at ON login_events(at);
CREATE TABLE IF NOT EXISTS play_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user TEXT NOT NULL,
	track TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	artist TEXT NOT NULL DEFAULT '',
	device TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	bytes INTEGER NOT NULL DEFAULT 0,
	saver INTEGER NOT NULL DEFAULT 0,
	seconds INTEGER NOT NULL DEFAULT 0,
	playing INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS play_events_user ON play_events(user, track, updated_at);
CREATE INDEX IF NOT EXISTS play_events_started ON play_events(started_at);
CREATE TABLE IF NOT EXISTS user_seen (
	user TEXT PRIMARY KEY,
	last_seen INTEGER NOT NULL,
	device TEXT NOT NULL DEFAULT '',
	prev_seen INTEGER NOT NULL DEFAULT 0
);`

func (s *Store) RecordLogin(user string, at time.Time, ok bool, device string, remember bool) error {
	_, err := s.db.Exec(`INSERT INTO login_events (user, at, ok, device, remember) VALUES (?, ?, ?, ?, ?)`,
		user, at.Unix(), ok, device, remember)
	return err
}

const newVisitGap = 30 * time.Minute

func migrateUserSeen(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('user_seen') WHERE name = 'prev_seen'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE user_seen ADD COLUMN prev_seen INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE user_seen SET prev_seen = last_seen`)
	return err
}

func (s *Store) TouchSeen(user string, at time.Time, device string) error {
	_, err := s.db.Exec(`INSERT INTO user_seen (user, last_seen, device, prev_seen) VALUES (?, ?, ?, ?)
ON CONFLICT(user) DO UPDATE SET
prev_seen = CASE WHEN excluded.last_seen - user_seen.last_seen > ? THEN user_seen.last_seen ELSE user_seen.prev_seen END,
last_seen = MAX(user_seen.last_seen, excluded.last_seen), device = excluded.device`,
		user, at.Unix(), device, at.Unix(), int64(newVisitGap.Seconds()))
	return err
}

func (s *Store) NewSince(user string, now time.Time) int64 {
	var prev int64
	err := s.db.QueryRow(`SELECT prev_seen FROM user_seen WHERE user = ?`, user).Scan(&prev)
	if err != nil || prev <= 0 {
		return now.Unix()
	}
	return prev
}

func (s *Store) OwnAlbums(user string) []string {
	out := []string{}
	rows, err := s.db.Query(`SELECT DISTINCT lower(trim(artist)) || '|' || lower(trim(album)) FROM uploads
WHERE user = ? AND album != '' AND status = 'in-library'`, user)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out
}

func (s *Store) AddStreamBytes(user, track, device string, at time.Time, n int64, saver bool) error {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM play_events WHERE user = ? AND track = ? AND updated_at >= ? ORDER BY updated_at DESC LIMIT 1`,
		user, track, at.Add(-playMergeGap).Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.db.Exec(`INSERT INTO play_events (user, track, device, started_at, updated_at, bytes, saver) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			user, track, device, at.Unix(), at.Unix(), n, saver)
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE play_events SET bytes = bytes + ?, updated_at = ?, saver = MAX(saver, ?) WHERE id = ?`, n, at.Unix(), saver, id)
	return err
}

func (s *Store) RecordProgress(user, track, title, artist string, at time.Time, seconds int, playing bool) error {
	if seconds < 0 {
		seconds = 0
	}
	if seconds > maxPlaySeconds {
		seconds = maxPlaySeconds
	}
	res, err := s.db.Exec(`UPDATE play_events SET seconds = MAX(seconds, ?), playing = ?, updated_at = ?,
title = CASE WHEN ? != '' THEN ? ELSE title END, artist = CASE WHEN ? != '' THEN ? ELSE artist END
WHERE id = (SELECT id FROM play_events WHERE user = ? AND track = ? AND updated_at >= ? ORDER BY updated_at DESC LIMIT 1)`,
		seconds, playing, at.Unix(), title, title, artist, artist, user, track, at.Add(-playMergeGap).Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) PurgeStats(before time.Time) error {
	cut := before.Unix()
	if _, err := s.db.Exec(`DELETE FROM login_events WHERE at < ?`, cut); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM play_events WHERE started_at < ?`, cut); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM start_events WHERE at < ?`, cut); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM user_seen WHERE last_seen < ?`, cut)
	return err
}

type UserStats struct {
	User      string   `json:"user"`
	LastLogin int64    `json:"lastLogin"`
	LastSeen  int64    `json:"lastSeen"`
	Devices   []string `json:"devices"`
	Plays     int      `json:"plays"`
	Seconds   int64    `json:"seconds"`
	Bytes     int64    `json:"bytes"`
	Daily     []int64  `json:"daily"`
}

type LoginEvent struct {
	User     string `json:"user"`
	At       int64  `json:"at"`
	OK       bool   `json:"ok"`
	Device   string `json:"device"`
	Remember bool   `json:"remember"`
}

type NowPlaying struct {
	User      string `json:"user"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Device    string `json:"device"`
	Saver     bool   `json:"saver"`
	UpdatedAt int64  `json:"updatedAt"`
}

type StatsTotals struct {
	Plays   int   `json:"plays"`
	Seconds int64 `json:"seconds"`
	Bytes   int64 `json:"bytes"`
	Logins  int   `json:"logins"`
	Failed  int   `json:"failed"`
	Active  int   `json:"active"`
}

type Dashboard struct {
	From       int64        `json:"from"`
	Days       []string     `json:"days"`
	Totals     StatsTotals  `json:"totals"`
	Users      []*UserStats `json:"users"`
	NowPlaying []NowPlaying `json:"nowPlaying"`
	Logins     []LoginEvent `json:"logins"`
	Starts     *StartStats  `json:"starts"`
}

func scanEach(rows *sql.Rows, err error, fn func(*sql.Rows) error) error {
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) Dashboard(now time.Time, days int) (*Dashboard, error) {
	y, m, d := now.Date()
	first := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	from := first.Unix()
	out := &Dashboard{From: from, Users: []*UserStats{}, NowPlaying: []NowPlaying{}, Logins: []LoginEvent{}}
	for i := 0; i < days; i++ {
		out.Days = append(out.Days, first.AddDate(0, 0, i).Format("2006-01-02"))
	}
	users := map[string]*UserStats{}
	devices := map[string]map[string]bool{}
	get := func(u string) *UserStats {
		if us, ok := users[u]; ok {
			return us
		}
		us := &UserStats{User: u, Devices: []string{}, Daily: make([]int64, days)}
		users[u] = us
		return us
	}
	addDevice := func(u, dev string) {
		if dev == "" {
			return
		}
		if devices[u] == nil {
			devices[u] = map[string]bool{}
		}
		devices[u][dev] = true
	}

	rows, err := s.db.Query(`SELECT user, at, device FROM login_events WHERE ok = 1`)
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var u, dev string
		var at int64
		if err := r.Scan(&u, &at, &dev); err != nil {
			return err
		}
		us := get(u)
		if at > us.LastLogin {
			us.LastLogin = at
		}
		addDevice(u, dev)
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT user, last_seen, device FROM user_seen`)
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var u, dev string
		var at int64
		if err := r.Scan(&u, &at, &dev); err != nil {
			return err
		}
		get(u).LastSeen = at
		addDevice(u, dev)
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT user, started_at, bytes, seconds FROM play_events WHERE started_at >= ?`, from)
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var u string
		var at, b, sec int64
		if err := r.Scan(&u, &at, &b, &sec); err != nil {
			return err
		}
		us := get(u)
		us.Plays++
		us.Seconds += sec
		us.Bytes += b
		for i := days - 1; i >= 0; i-- {
			if at >= first.AddDate(0, 0, i).Unix() {
				us.Daily[i] += sec
				break
			}
		}
		out.Totals.Plays++
		out.Totals.Seconds += sec
		out.Totals.Bytes += b
		return nil
	}); err != nil {
		return nil, err
	}

	if err := s.db.QueryRow(`SELECT COALESCE(SUM(ok = 1), 0), COALESCE(SUM(ok = 0), 0) FROM login_events WHERE at >= ?`, from).
		Scan(&out.Totals.Logins, &out.Totals.Failed); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	rows, err = s.db.Query(`SELECT user, title, artist, device, saver, updated_at FROM play_events WHERE playing = 1 AND updated_at >= ? ORDER BY updated_at DESC`,
		now.Add(-nowPlayingGap).Unix())
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var np NowPlaying
		if err := r.Scan(&np.User, &np.Title, &np.Artist, &np.Device, &np.Saver, &np.UpdatedAt); err != nil {
			return err
		}
		if !seen[np.User] {
			seen[np.User] = true
			out.NowPlaying = append(out.NowPlaying, np)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT user, at, ok, device, remember FROM login_events ORDER BY at DESC, id DESC LIMIT 50`)
	if err := scanEach(rows, err, func(r *sql.Rows) error {
		var e LoginEvent
		if err := r.Scan(&e.User, &e.At, &e.OK, &e.Device, &e.Remember); err != nil {
			return err
		}
		out.Logins = append(out.Logins, e)
		return nil
	}); err != nil {
		return nil, err
	}

	for u, us := range users {
		for dv := range devices[u] {
			us.Devices = append(us.Devices, dv)
		}
		sort.Strings(us.Devices)
		if us.Plays > 0 {
			out.Totals.Active++
		}
		out.Users = append(out.Users, us)
	}
	sort.Slice(out.Users, func(i, j int) bool {
		if out.Users[i].Seconds != out.Users[j].Seconds {
			return out.Users[i].Seconds > out.Users[j].Seconds
		}
		return out.Users[i].User < out.Users[j].User
	})
	starts, err := s.StartStats(now)
	if err != nil {
		return nil, err
	}
	out.Starts = starts
	return out, nil
}

func deviceName(ua string) string {
	if ua == "" {
		return ""
	}
	osName := "Other"
	switch {
	case strings.Contains(ua, "iPhone"):
		osName = "iPhone"
	case strings.Contains(ua, "iPad"):
		osName = "iPad"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Macintosh"), strings.Contains(ua, "Mac OS X"):
		osName = "Mac"
	case strings.Contains(ua, "CrOS"):
		osName = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	browser := ""
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		browser = "Firefox"
	case strings.Contains(ua, "OPR/"):
		browser = "Opera"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	if browser == "" {
		return osName
	}
	return osName + " · " + browser
}

type seenThrottle struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (t *seenThrottle) due(user string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	if now.Sub(t.last[user]) < seenEvery {
		return false
	}
	t.last[user] = now
	return true
}

func (a *App) markSeen(r *http.Request, user string) {
	now := time.Now()
	if !a.seen.due(user, now) {
		return
	}
	a.store.TouchSeen(user, now, deviceName(r.UserAgent()))
}

func (a *App) recordStream(r *http.Request, user, track string, n int64, saver bool) {
	if n <= 0 {
		return
	}
	a.store.AddStreamBytes(user, track, deviceName(r.UserAgent()), time.Now(), n, saver)
}

func (a *App) handleProgress(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	var body struct {
		Seconds float64 `json:"seconds"`
		Playing bool    `json:"playing"`
		Title   string  `json:"title"`
		Artist  string  `json:"artist"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	err := a.store.RecordProgress(user, id, clip(body.Title, 200), clip(body.Artist, 200), time.Now(), int(body.Seconds), body.Playing)
	if err != nil && !errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "stats")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAdminStats(w http.ResponseWriter, r *http.Request, user string) {
	if !a.cachedAdmin(r) {
		writeError(w, http.StatusForbidden, "admin-only")
		return
	}
	d, err := a.store.Dashboard(time.Now(), 7)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stats")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
