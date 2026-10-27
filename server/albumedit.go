package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errScanOff = errors.New("scan password not set")

type albumEdit struct {
	Album       *string     `json:"album"`
	AlbumArtist *string     `json:"albumArtist"`
	Year        *string     `json:"year"`
	Genre       *string     `json:"genre"`
	Vinyl       *bool       `json:"vinyl"`
	Tracks      []trackEdit `json:"tracks"`
}

type trackEdit struct {
	ID     string  `json:"id"`
	Title  *string `json:"title"`
	Artist *string `json:"artist"`
}

func joinArtists(s string) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ";") {
		p = cleanTag(p)
		if p == "" || seen[strings.ToLower(p)] {
			continue
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
		if len(out) == maxArtists {
			break
		}
	}
	return strings.Join(out, artistSep)
}

func validYear(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (e *albumEdit) albumTags() (map[string]string, bool) {
	out := map[string]string{}
	if e.Album != nil {
		v := cleanTag(*e.Album)
		if v == "" {
			return nil, false
		}
		out["album"] = v
	}
	if e.AlbumArtist != nil {
		v := joinArtists(*e.AlbumArtist)
		if v == "" {
			return nil, false
		}
		out["albumartist"] = v
	}
	if e.Year != nil {
		v := cleanTag(*e.Year)
		if !validYear(v) {
			return nil, false
		}
		out["year"] = v
	}
	if e.Genre != nil {
		out["genre"] = cleanTag(*e.Genre)
	}
	return out, true
}

func (t *trackEdit) tags() (map[string]string, bool) {
	out := map[string]string{}
	if t.Title != nil {
		v := cleanTag(*t.Title)
		if v == "" {
			return nil, false
		}
		out["title"] = v
	}
	if t.Artist != nil {
		v := joinArtists(*t.Artist)
		if v == "" {
			return nil, false
		}
		out["artist"] = v
	}
	return out, true
}

func (s *Scanner) StartNow(ctx context.Context) error {
	if s.pass == "" {
		return errScanOff
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.nav.StartScan(ctx, NewCreds(s.user, s.pass))
}

func retagFile(ctx context.Context, path string, typed map[string]string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if !audioExt(path) {
		return fmt.Errorf("unsupported format %q", ext)
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".edit"+ext)
	if err := writeTags(ctx, path, tmp, ext, typed); err != nil {
		return err
	}
	if err := os.Chmod(tmp, st.Mode().Perm()); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (a *App) handleAlbumEdit(w http.ResponseWriter, r *http.Request, user string) {
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
		writeError(w, http.StatusForbidden, "admin-only")
		return
	}
	var in albumEdit
	if err == nil && !readJSON(w, r, &in) {
		return
	}
	var songs []navSong
	if err == nil {
		songs, err = a.loadAlbum(r.Context(), creds, id)
	}
	switch {
	case errors.Is(err, ErrBadLogin):
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not-found")
		return
	case err != nil:
		log.Printf("edit album %s: %v", id, err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	if len(songs) == 0 {
		writeError(w, http.StatusNotFound, "not-found")
		return
	}
	common, ok := in.albumTags()
	if !ok {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	perTrack := map[string]map[string]string{}
	for i := range in.Tracks {
		tags, ok := in.Tracks[i].tags()
		if !ok {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		perTrack[in.Tracks[i].ID] = tags
	}
	known := map[string]bool{}
	for _, s := range songs {
		known[s.ID] = true
	}
	for tid := range perTrack {
		if !known[tid] {
			writeError(w, http.StatusBadRequest, "unknown-track")
			return
		}
	}
	type job struct {
		path  string
		typed map[string]string
	}
	var jobs []job
	for _, s := range songs {
		typed := map[string]string{}
		for k, v := range common {
			typed[k] = v
		}
		for k, v := range perTrack[s.ID] {
			typed[k] = v
		}
		if len(typed) == 0 {
			continue
		}
		f, err := a.resolveSongPath(s.Path)
		switch {
		case errors.Is(err, errOutside):
			log.Printf("edit album %s: refused path %q", id, s.Path)
			writeError(w, http.StatusForbidden, "outside-library")
			return
		case errors.Is(err, ErrNotFound):
			log.Printf("edit album %s: %v", id, err)
			writeError(w, http.StatusConflict, "file-not-found")
			return
		case err != nil:
			log.Printf("edit album %s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "resolve")
			return
		}
		if !audioExt(f) {
			writeError(w, http.StatusUnprocessableEntity, "unsupported-format")
			return
		}
		jobs = append(jobs, job{f, typed})
	}
	if in.Vinyl != nil {
		folder := a.albumFolder(songs)
		if folder == "" {
			writeError(w, http.StatusConflict, "file-not-found")
			return
		}
		if err := a.store.SetVinylFolder(folder, id, *in.Vinyl, time.Now()); err != nil {
			log.Printf("edit album %s: vinyl: %v", id, err)
			writeError(w, http.StatusInternalServerError, "store")
			return
		}
		log.Printf("edit album %s by %s: vinyl %v (%s)", id, user, *in.Vinyl, folder)
	}
	updated := 0
	var failed error
	for _, j := range jobs {
		if err := retagFile(r.Context(), j.path, j.typed); err != nil {
			failed = err
			log.Printf("edit album %s: %s: %v", id, j.path, err)
			break
		}
		updated++
	}
	scan := "started"
	if updated > 0 {
		if err := a.scanner.StartNow(r.Context()); err != nil {
			scan = "failed"
			if errors.Is(err, errScanOff) {
				scan = "off"
			}
			log.Printf("edit album %s: scan: %v", id, err)
		}
	} else {
		scan = "skipped"
	}
	log.Printf("edit album %s by %s: %d files retagged, scan %s", id, user, updated, scan)
	if failed != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "write-failed", "updated": updated, "scan": scan})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated, "scan": scan})
}
