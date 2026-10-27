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
)

var backCoverNames = []string{"back.jpg", "back.png"}

func backCoverFile(dir string) string {
	if dir == "" {
		return ""
	}
	for _, n := range backCoverNames {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return p
		}
	}
	return ""
}

func saveBackCover(ctx context.Context, dir string, data []byte) error {
	if sniffImage(data) == "" {
		return errors.New("unsupported back cover type")
	}
	return replaceImage(ctx, dir, "back", data)
}

func (a *App) libraryFolderDir(folder string) string {
	if folder == "" {
		return ""
	}
	slash := filepath.ToSlash(folder)
	for _, seg := range strings.Split(slash, "/") {
		if seg == ".." {
			return ""
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+slash), "/")
	if clean == "" {
		return ""
	}
	dir := filepath.Join(a.cfg.LibraryDir(), filepath.FromSlash(clean))
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

func (a *App) SetBackCover(ctx context.Context, folder string, data []byte) error {
	dir := a.libraryFolderDir(folder)
	if dir == "" {
		return os.ErrNotExist
	}
	return saveBackCover(ctx, dir, data)
}

func (a *App) handleBackCover(w http.ResponseWriter, r *http.Request, user string) {
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
		log.Printf("back cover getAlbum.view: %v", err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	var songs struct {
		Song []navSong `json:"song"`
	}
	json.Unmarshal(body, &songs)
	file := backCoverFile(a.libraryFolderDir(a.albumFolder(songs.Song)))
	f, err := os.Open(file)
	if file == "" || err != nil {
		w.Header().Set("Cache-Control", "private, max-age=300")
		writeError(w, http.StatusNotFound, "not-found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "not-found")
		return
	}
	if strings.HasSuffix(file, ".png") {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
