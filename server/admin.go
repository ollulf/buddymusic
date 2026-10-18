package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const adminCacheTTL = 5 * time.Minute

type adminEntry struct {
	admin bool
	until time.Time
}

type adminCache struct {
	mu      sync.Mutex
	entries map[string]adminEntry
}

func (c *adminCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.until) {
		return false, false
	}
	return e.admin, true
}

func (c *adminCache) put(key string, admin bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]adminEntry{}
	}
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.until) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = adminEntry{admin: admin, until: now.Add(adminCacheTTL)}
}

var errNotAdmin = errors.New("not an admin")

func (a *App) checkAdmin(ctx context.Context, creds SubsonicCreds) (bool, error) {
	body, err := a.nav.raw(ctx, "getUser.view", "user", creds, url.Values{"username": {creds.User}})
	if err != nil {
		return false, err
	}
	var u struct {
		AdminRole bool `json:"adminRole"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return false, err
	}
	return u.AdminRole, nil
}

func (a *App) cachedAdmin(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	key := hashSessionID(c.Value)
	if admin, ok := a.admins.get(key); ok {
		return admin
	}
	creds, err := a.sessionCreds(r)
	if err != nil {
		return false
	}
	admin, err := a.checkAdmin(r.Context(), creds)
	if err != nil {
		log.Printf("admin check: %v", err)
		return false
	}
	a.admins.put(key, admin)
	return admin
}

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".mp4": true, ".aac": true, ".ogg": true, ".oga": true,
	".opus": true, ".wav": true, ".aif": true, ".aiff": true, ".wma": true, ".ape": true, ".wv": true,
	".dsf": true, ".dff": true, ".mpc": true,
}

type navSong struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

var errOutside = errors.New("path outside the library")

func (a *App) resolveSongPath(p string) (string, error) {
	lib, err := filepath.EvalSymlinks(a.cfg.LibraryDir())
	if err != nil {
		return "", err
	}
	if p == "" || strings.ContainsRune(p, 0) {
		return "", errOutside
	}
	var candidates []string
	if filepath.IsAbs(p) {
		c := filepath.Clean(p)
		if !inside(a.cfg.LibraryDir(), c) && !inside(lib, c) {
			return "", errOutside
		}
		candidates = []string{c}
	} else {
		rel := filepath.Clean(filepath.FromSlash(p))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", errOutside
		}
		candidates = append(candidates, filepath.Join(a.cfg.LibraryDir(), rel))
		if entries, err := os.ReadDir(a.cfg.LibraryDir()); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					candidates = append(candidates, filepath.Join(a.cfg.LibraryDir(), e.Name(), rel))
				}
			}
		}
	}
	var found []string
	for _, c := range candidates {
		real, err := filepath.EvalSymlinks(c)
		if err != nil {
			continue
		}
		if !inside(lib, real) {
			return "", errOutside
		}
		st, err := os.Lstat(real)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		found = append(found, real)
	}
	if len(found) != 1 {
		return "", fmt.Errorf("%w: %d files match %q", ErrNotFound, len(found), p)
	}
	return found[0], nil
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (a *App) trashDir(id string) string {
	return filepath.Join(a.cfg.MusicRoot, "_trash", time.Now().UTC().Format("20060102-150405")+"-"+id)
}

func (a *App) moveToTrash(lib, trash, src string) (string, error) {
	rel, err := filepath.Rel(lib, src)
	if err != nil || !inside(lib, src) {
		return "", errOutside
	}
	dst := filepath.Join(trash, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o775); err != nil {
		return "", err
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", errExists
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func hasAudio(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if e.IsDir() || audioExts[strings.ToLower(filepath.Ext(e.Name()))] {
			return true
		}
	}
	return false
}

func (a *App) tidyDir(lib, trash, dir string) []string {
	var moved []string
	if !inside(lib, dir) || hasAudio(dir) {
		return nil
	}
	for _, n := range coverNames {
		p := filepath.Join(dir, n)
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() {
			if rel, err := a.moveToTrash(lib, trash, p); err == nil {
				moved = append(moved, rel)
			}
		}
	}
	for d := dir; inside(lib, d); d = filepath.Dir(d) {
		rel, _ := filepath.Rel(lib, d)
		if len(strings.Split(rel, string(filepath.Separator))) < 2 {
			break
		}
		if os.Remove(d) != nil {
			break
		}
	}
	return moved
}

func (a *App) deleteSongs(id string, songs []navSong) (int, any) {
	lib, err := filepath.EvalSymlinks(a.cfg.LibraryDir())
	if err != nil {
		return http.StatusInternalServerError, map[string]string{"error": "library"}
	}
	var files []string
	for _, s := range songs {
		f, err := a.resolveSongPath(s.Path)
		switch {
		case errors.Is(err, errOutside):
			log.Printf("delete %s: refused path %q", id, s.Path)
			return http.StatusForbidden, map[string]string{"error": "outside-library"}
		case errors.Is(err, ErrNotFound):
			log.Printf("delete %s: %v", id, err)
			return http.StatusConflict, map[string]string{"error": "file-not-found"}
		case err != nil:
			log.Printf("delete %s: %v", id, err)
			return http.StatusInternalServerError, map[string]string{"error": "resolve"}
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return http.StatusNotFound, map[string]string{"error": "not-found"}
	}
	trash := a.trashDir(id)
	moved := []string{}
	dirs := map[string]bool{}
	for _, f := range files {
		rel, err := a.moveToTrash(lib, trash, f)
		if err != nil {
			log.Printf("delete %s: move %s: %v", id, f, err)
			a.scanner.Trigger()
			return http.StatusInternalServerError, map[string]any{"error": "move-failed", "moved": moved}
		}
		moved = append(moved, rel)
		dirs[filepath.Dir(f)] = true
	}
	keys := make([]string, 0, len(dirs))
	for d := range dirs {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	for _, d := range keys {
		moved = append(moved, a.tidyDir(lib, trash, d)...)
	}
	log.Printf("delete %s: moved %d files to %s", id, len(moved), trash)
	a.scanner.Trigger()
	return http.StatusOK, map[string]any{"moved": moved}
}

func (a *App) adminDelete(load func(ctx context.Context, creds SubsonicCreds, id string) ([]navSong, error)) userHandler {
	return func(w http.ResponseWriter, r *http.Request, user string) {
		id := r.PathValue("id")
		if !validID(id) {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		creds, err := a.sessionCreds(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		admin, err := a.checkAdmin(r.Context(), creds)
		if err == nil && !admin {
			err = errNotAdmin
		}
		if err == nil {
			var songs []navSong
			songs, err = load(r.Context(), creds, id)
			if err == nil {
				status, body := a.deleteSongs(id, songs)
				writeJSON(w, status, body)
				return
			}
		}
		switch {
		case errors.Is(err, errNotAdmin):
			writeError(w, http.StatusForbidden, "admin-only")
		case errors.Is(err, ErrBadLogin):
			writeError(w, http.StatusUnauthorized, "unauthorized")
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not-found")
		default:
			log.Printf("delete %s: %v", id, err)
			writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		}
	}
}

func (a *App) loadTrack(ctx context.Context, creds SubsonicCreds, id string) ([]navSong, error) {
	body, err := a.nav.raw(ctx, "getSong.view", "song", creds, url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	var s navSong
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, err
	}
	return []navSong{s}, nil
}

func (a *App) loadAlbum(ctx context.Context, creds SubsonicCreds, id string) ([]navSong, error) {
	body, err := a.nav.raw(ctx, "getAlbum.view", "album", creds, url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	var al struct {
		Song []navSong `json:"song"`
	}
	if err := json.Unmarshal(body, &al); err != nil {
		return nil, err
	}
	return al.Song, nil
}
