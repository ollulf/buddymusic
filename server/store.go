package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Upload struct {
	ID          string   `json:"id"`
	User        string   `json:"-"`
	Filename    string   `json:"filename"`
	Status      string   `json:"status"`
	Missing     []string `json:"missing"`
	Artist      string   `json:"artist,omitempty"`
	Album       string   `json:"album,omitempty"`
	Title       string   `json:"title,omitempty"`
	TrackNo     int      `json:"trackNo,omitempty"`
	LibraryPath string   `json:"libraryPath,omitempty"`
	CreatedAt   int64    `json:"createdAt"`
}

var ErrNotFound = errors.New("not found")

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS uploads (
	id TEXT PRIMARY KEY,
	user TEXT NOT NULL,
	filename TEXT NOT NULL,
	status TEXT NOT NULL,
	missing TEXT NOT NULL DEFAULT '[]',
	artist TEXT NOT NULL DEFAULT '',
	album TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	track_no INTEGER NOT NULL DEFAULT 0,
	library_path TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS uploads_user ON uploads(user, created_at);
CREATE TABLE IF NOT EXISTS sessions (
	id_hash TEXT PRIMARY KEY,
	user TEXT NOT NULL,
	secret BLOB NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS avatars (
	user TEXT PRIMARY KEY,
	mime TEXT NOT NULL,
	data BLOB NOT NULL,
	updated_at INTEGER NOT NULL
);` + statsSchema)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SaveUpload(u *Upload) error {
	if u.Missing == nil {
		u.Missing = []string{}
	}
	missing, _ := json.Marshal(u.Missing)
	_, err := s.db.Exec(`INSERT INTO uploads (id, user, filename, status, missing, artist, album, title, track_no, library_path, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, missing=excluded.missing, artist=excluded.artist,
album=excluded.album, title=excluded.title, track_no=excluded.track_no, library_path=excluded.library_path`,
		u.ID, u.User, u.Filename, u.Status, string(missing), u.Artist, u.Album, u.Title, u.TrackNo, u.LibraryPath, u.CreatedAt)
	return err
}

const uploadCols = `id, user, filename, status, missing, artist, album, title, track_no, library_path, created_at`

func scanUpload(row interface{ Scan(...any) error }) (*Upload, error) {
	var u Upload
	var missing string
	if err := row.Scan(&u.ID, &u.User, &u.Filename, &u.Status, &missing, &u.Artist, &u.Album, &u.Title, &u.TrackNo, &u.LibraryPath, &u.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(missing), &u.Missing)
	if u.Missing == nil {
		u.Missing = []string{}
	}
	return &u, nil
}

func (s *Store) GetUpload(id, user string) (*Upload, error) {
	u, err := scanUpload(s.db.QueryRow(`SELECT `+uploadCols+` FROM uploads WHERE id = ? AND user = ?`, id, user))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) RecentUploads(user string, limit int) ([]*Upload, error) {
	rows, err := s.db.Query(`SELECT `+uploadCols+` FROM uploads WHERE user = ? ORDER BY created_at DESC LIMIT ?`, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Upload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SaveSession(idHash, user string, secret []byte, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.Exec(`INSERT INTO sessions (id_hash, user, secret, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		idHash, user, secret, now.Unix(), now.Add(ttl).Unix())
	return err
}

func (s *Store) GetSession(idHash string) (user string, secret []byte, err error) {
	err = s.db.QueryRow(`SELECT user, secret FROM sessions WHERE id_hash = ? AND expires_at > ?`, idHash, time.Now().Unix()).Scan(&user, &secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	return user, secret, err
}

func (s *Store) SessionExpiry(idHash string) (time.Time, error) {
	var exp int64
	err := s.db.QueryRow(`SELECT expires_at FROM sessions WHERE id_hash = ?`, idHash).Scan(&exp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	return time.Unix(exp, 0), err
}

func (s *Store) DeleteSession(idHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

func (s *Store) PurgeSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	return err
}
