package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
		if d := a.safeAlbumDir(lib, s.Path); d != "" {
			if !seenE[d] {
				seenE[d] = true
				exact = append(exact, d)
			}
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

func (a *App) safeAlbumDir(lib, songPath string) string {
	if lib == "" {
		return ""
	}
	dir := path.Dir(path.Clean("/" + filepath.ToSlash(songPath)))
	var segs []string
	for _, seg := range strings.Split(strings.TrimPrefix(dir, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
		segs = append(segs, safeSegment(seg))
	}
	if len(segs) == 0 {
		return ""
	}
	rel := path.Join(segs...)
	cands := []string{rel}
	if users, err := os.ReadDir(lib); err == nil {
		for _, u := range users {
			if u.IsDir() && !strings.HasPrefix(u.Name(), ".") {
				cands = append(cands, path.Join(u.Name(), rel))
			}
		}
	}
	found := ""
	for _, c := range cands {
		real, err := filepath.EvalSymlinks(filepath.Join(lib, filepath.FromSlash(c)))
		if err != nil || !inside(lib, real) {
			continue
		}
		if st, err := os.Stat(real); err != nil || !st.IsDir() {
			continue
		}
		r, err := filepath.Rel(lib, real)
		if err != nil {
			continue
		}
		r = filepath.ToSlash(r)
		if found != "" && found != r {
			return ""
		}
		found = r
	}
	return found
}

func (a *App) albumFolder(songs []navSong) string {
	exact, suffix := a.albumDirs(songs)
	dirs := exact
	if len(dirs) == 0 {
		dirs = suffix
	}
	if len(dirs) == 0 {
		return ""
	}
	common := strings.Split(dirs[0], "/")
	for _, d := range dirs[1:] {
		parts := strings.Split(d, "/")
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}

func (a *App) SetVinylFolder(folder string, on bool) error {
	return a.store.SetVinylFolder(path.Clean(filepath.ToSlash(folder)), "", on, time.Now())
}

const (
	vinylRetry     = 2 * time.Minute
	vinylMaxLookup = 5
	vinylTimeout   = 3 * time.Second
)

type vinylThrottle struct {
	mu    sync.Mutex
	tried map[string]time.Time
}

func (v *vinylThrottle) due(folder string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if at, ok := v.tried[folder]; ok && now.Sub(at) < vinylRetry {
		return false
	}
	if v.tried == nil {
		v.tried = map[string]time.Time{}
	}
	v.tried[folder] = now
	return true
}

func (a *App) resolveVinylAlbums(ctx context.Context, creds SubsonicCreds) {
	folders := a.store.UnresolvedVinyl()
	if len(folders) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, vinylTimeout)
	defer cancel()
	now := time.Now()
	lookups := 0
	for _, folder := range folders {
		if lookups >= vinylMaxLookup || ctx.Err() != nil {
			return
		}
		if !a.vinylTry.due(folder, now) {
			continue
		}
		lookups++
		id, err := a.albumIDForFolder(ctx, creds, folder)
		if err != nil {
			log.Printf("vinyl %s: resolve: %v", folder, err)
			continue
		}
		if id != "" {
			a.store.VinylFolder(folder, id)
		}
	}
}

func (a *App) albumIDForFolder(ctx context.Context, creds SubsonicCreds, folder string) (string, error) {
	query := a.store.FolderAlbumName(folder)
	if query == "" {
		query = path.Base(folder)
	}
	q := url.Values{"query": {query}, "artistCount": {"0"}, "albumCount": {"0"}, "songCount": {"50"}}
	raw, err := a.nav.raw(ctx, "search3.view", "searchResult3", creds, q)
	if err != nil {
		return "", err
	}
	var res struct {
		Song []struct {
			navSong
			AlbumID string `json:"albumId"`
		} `json:"song"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	for _, s := range res.Song {
		if s.AlbumID == "" {
			continue
		}
		exact, suffix := a.albumDirs([]navSong{s.navSong})
		for _, d := range exact {
			if d == folder || strings.HasPrefix(d, folder+"/") {
				return s.AlbumID, nil
			}
		}
		for _, d := range suffix {
			if d == folder || strings.HasPrefix(d, folder+"/") || strings.HasSuffix(folder, "/"+d) {
				return s.AlbumID, nil
			}
		}
	}
	return "", nil
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

func (a *App) handleAlbumRenew(w http.ResponseWriter, r *http.Request, user string) {
	if !a.cachedAdmin(r) {
		writeError(w, http.StatusForbidden, "admin-only")
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	if err := a.store.RenewAlbum(id, time.Now()); err != nil {
		log.Printf("renew album: %v", err)
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
	folder := a.albumFolder(songs.Song)
	if folder != "" && a.store.VinylFolder(folder, id) {
		al["vinyl"] = json.RawMessage("true")
	}
	if dir := a.libraryFolderDir(folder); dir != "" {
		if v := backCoverVersion(dir); v != 0 {
			al["hasBack"] = json.RawMessage("true")
			al["backV"], _ = json.Marshal(v)
		}
	}
	if tags := albumGenres(body); len(tags) > 0 {
		al["genreTags"], _ = json.Marshal(tags)
	}
	out, err := json.Marshal(al)
	if err != nil {
		w.Write(body)
		return
	}
	w.Write(out)
}
