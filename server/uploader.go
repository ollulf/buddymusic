package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const uploadsLibraryIndex = `
CREATE INDEX IF NOT EXISTS uploads_library_path ON uploads(library_path);`

func (s *Store) AlbumUploaders(exact, suffix []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(user string) {
		if !seen[user] {
			seen[user] = true
			out = append(out, user)
		}
	}
	for _, dir := range exact {
		rows, err := s.db.Query(`SELECT user, library_path FROM uploads WHERE status = 'in-library' AND library_path >= ? AND library_path < ? ORDER BY created_at, id`, dir+"/", dir+"0")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var user, lp string
			if err := rows.Scan(&user, &lp); err != nil {
				rows.Close()
				return nil, err
			}
			if path.Dir(filepath.ToSlash(lp)) == dir {
				add(user)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(suffix) == 0 {
		return out, nil
	}
	want := map[string]bool{}
	for _, d := range suffix {
		want[d] = true
	}
	rows, err := s.db.Query(`SELECT user, library_path FROM uploads WHERE status = 'in-library' AND library_path != '' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var user, lp string
		if err := rows.Scan(&user, &lp); err != nil {
			return nil, err
		}
		dir := path.Dir(filepath.ToSlash(lp))
		if want[dir] {
			add(user)
			continue
		}
		if i := strings.IndexByte(dir, '/'); i > 0 && want[dir[i+1:]] {
			add(user)
		}
	}
	return out, rows.Err()
}

func (a *App) albumDirs(songs []navSong) (exact, suffix []string) {
	lib, _ := filepath.EvalSymlinks(a.cfg.LibraryDir())
	seenE, seenS := map[string]bool{}, map[string]bool{}
	for _, s := range songs {
		if real, err := a.resolveSongPath(s.Path); err == nil && lib != "" {
			if rel, err := filepath.Rel(lib, real); err == nil {
				d := path.Dir(filepath.ToSlash(rel))
				if d != "." && !seenE[d] {
					seenE[d] = true
					exact = append(exact, d)
				}
				continue
			}
		}
		if s.Path == "" || filepath.IsAbs(s.Path) {
			continue
		}
		d := path.Dir(strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(s.Path)), "/"))
		if d == "." || seenS[d] {
			continue
		}
		seenS[d] = true
		suffix = append(suffix, d)
	}
	return exact, suffix
}

func (a *App) handleAlbumOpened(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	if err := a.store.MarkAlbumOpened(user, id, time.Now()); err != nil {
		log.Printf("mark album opened: %v", err)
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAlbum(w http.ResponseWriter, r *http.Request, user string) {
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
	body, err := a.nav.raw(r.Context(), "getAlbum.view", "album", creds, url.Values{"id": {id}})
	switch {
	case errors.Is(err, ErrBadLogin):
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not-found")
		return
	case err != nil:
		log.Printf("music getAlbum.view: %v", err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	var al map[string]json.RawMessage
	var songs struct {
		Song []navSong `json:"song"`
	}
	if json.Unmarshal(body, &al) != nil || json.Unmarshal(body, &songs) != nil {
		w.Write(body)
		return
	}
	exact, suffix := a.albumDirs(songs.Song)
	users, err := a.store.AlbumUploaders(exact, suffix)
	if err != nil {
		log.Printf("album %s uploaders: %v", id, err)
	}
	if len(users) > 0 {
		al["uploadedBy"], _ = json.Marshal(users)
	}
	out, err := json.Marshal(al)
	if err != nil {
		w.Write(body)
		return
	}
	w.Write(out)
}
