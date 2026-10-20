package main

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	maxAvatarBytes = 256 << 10
	maxAvatarSide  = 1024
)

type Avatar struct {
	Mime      string
	Data      []byte
	UpdatedAt int64
}

func (s *Store) SaveAvatar(user, mime string, data []byte) (int64, error) {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO avatars (user, mime, data, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(user) DO UPDATE SET mime=excluded.mime, data=excluded.data, updated_at=excluded.updated_at`, user, mime, data, now)
	return now, err
}

func (s *Store) GetAvatar(user string) (*Avatar, error) {
	var av Avatar
	err := s.db.QueryRow(`SELECT mime, data, updated_at FROM avatars WHERE user = ?`, user).Scan(&av.Mime, &av.Data, &av.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &av, err
}

func (s *Store) AvatarVersion(user string) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT updated_at FROM avatars WHERE user = ?`, user).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

func (s *Store) DeleteAvatar(user string) error {
	_, err := s.db.Exec(`DELETE FROM avatars WHERE user = ?`, user)
	return err
}

func avatarMime(data []byte) (string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		return "", errors.New("bad dimensions")
	}
	switch format {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	}
	return "", errors.New("unsupported format")
}

func (a *App) handleGetAvatar(w http.ResponseWriter, r *http.Request, user string) {
	av, err := a.store.GetAvatar(user)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "no-avatar")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	etag := `"` + strconv.FormatInt(av.UpdatedAt, 10) + `"`
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", av.Mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(av.Data)))
	w.Write(av.Data)
}

func (a *App) handlePutAvatar(w http.ResponseWriter, r *http.Request, user string) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAvatarBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too-large")
			return
		}
		writeError(w, http.StatusBadRequest, "bad-body")
		return
	}
	mime, err := avatarMime(data)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "bad-image")
		return
	}
	v, err := a.store.SaveAvatar(user, mime, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"avatar": v})
}

func (a *App) handleDeleteAvatar(w http.ResponseWriter, r *http.Request, user string) {
	if err := a.store.DeleteAvatar(user); err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
