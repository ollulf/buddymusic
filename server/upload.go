package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxTagsJSON  = 64 << 10
	maxCoverSize = 10 << 20
)

var errTooLarge = errors.New("too large")

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sniffAudio(head []byte, filename string) string {
	switch {
	case bytes.HasPrefix(head, []byte("fLaC")):
		return ".flac"
	case bytes.HasPrefix(head, []byte("ID3")):
		return ".mp3"
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return ".mp3"
	}
	return ""
}

func sniffImage(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return ".jpg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	}
	return ""
}

type uploadForm struct {
	path     string
	filename string
	ext      string
	tagsJSON []byte
	cover    []byte
	back     []byte
	vinyl    bool
}

func (f *uploadForm) cleanup() {
	if f.path != "" {
		os.Remove(f.path)
	}
}

func (a *App) readUploadForm(r *http.Request, dir, id string) (*uploadForm, int, string) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, http.StatusBadRequest, "bad-request"
	}
	f := &uploadForm{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			f.cleanup()
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return nil, http.StatusRequestEntityTooLarge, "too-large"
			}
			return nil, http.StatusBadRequest, "bad-request"
		}
		status, code := a.readPart(part, f, dir, id)
		part.Close()
		if status != 0 {
			f.cleanup()
			return nil, status, code
		}
	}
	if f.path == "" {
		return nil, http.StatusBadRequest, "no-file"
	}
	return f, 0, ""
}

func (a *App) readPart(part *multipart.Part, f *uploadForm, dir, id string) (int, string) {
	switch part.FormName() {
	case "file":
		if f.path != "" {
			return http.StatusBadRequest, "one-file-per-request"
		}
		f.filename = filepath.Base(part.FileName())
		f.path = filepath.Join(dir, id+".part")
		head, err := saveLimited(part, f.path, a.cfg.MaxUpload)
		if errors.Is(err, errTooLarge) {
			return http.StatusRequestEntityTooLarge, "too-large"
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return http.StatusRequestEntityTooLarge, "too-large"
			}
			log.Printf("upload %s: save: %v", id, err)
			return http.StatusInternalServerError, "save-failed"
		}
		f.ext = sniffAudio(head, f.filename)
		if f.ext == "" {
			return http.StatusUnsupportedMediaType, "unsupported-type"
		}
	case "tags":
		b, err := readLimited(part, maxTagsJSON)
		if err != nil {
			return http.StatusBadRequest, "bad-tags"
		}
		f.tagsJSON = b
	case "vinyl":
		b, err := readLimited(part, 16)
		if err != nil {
			return http.StatusBadRequest, "bad-request"
		}
		switch strings.ToLower(strings.TrimSpace(string(b))) {
		case "1", "true", "on", "yes":
			f.vinyl = true
		}
	case "back":
		b, err := readLimited(part, maxCoverSize)
		if errors.Is(err, errTooLarge) {
			return http.StatusRequestEntityTooLarge, "cover-too-large"
		}
		if err != nil {
			return http.StatusBadRequest, "bad-cover"
		}
		if len(b) > 0 {
			if sniffImage(b) == "" {
				return http.StatusUnsupportedMediaType, "unsupported-cover-type"
			}
			f.back = b
		}
	case "cover":
		b, err := readLimited(part, maxCoverSize)
		if errors.Is(err, errTooLarge) {
			return http.StatusRequestEntityTooLarge, "cover-too-large"
		}
		if err != nil {
			return http.StatusBadRequest, "bad-cover"
		}
		if len(b) > 0 {
			if sniffImage(b) == "" {
				return http.StatusUnsupportedMediaType, "unsupported-cover-type"
			}
			f.cover = b
		}
	default:
		io.Copy(io.Discard, io.LimitReader(part, maxTagsJSON))
	}
	return 0, ""
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

func saveLimited(r io.Reader, path string, limit int64) ([]byte, error) {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		out.Close()
		return nil, err
	}
	head = head[:n]
	if _, err := out.Write(head); err != nil {
		out.Close()
		return nil, err
	}
	written, err := io.Copy(out, io.LimitReader(r, limit-int64(n)+1))
	if err != nil {
		out.Close()
		return nil, err
	}
	if int64(n)+written > limit {
		out.Close()
		return nil, errTooLarge
	}
	return head, out.Close()
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request, user string) {
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUpload+2*maxCoverSize+maxTagsJSON+(1<<20))
	dir := filepath.Join(a.cfg.IncomingDir(), safeSegment(user))
	if err := os.MkdirAll(dir, 0o2775); err != nil {
		log.Printf("upload: mkdir: %v", err)
		writeError(w, http.StatusInternalServerError, "save-failed")
		return
	}
	id := newID()
	form, status, code := a.readUploadForm(r, dir, id)
	if status != 0 {
		writeError(w, status, code)
		return
	}
	stored := filepath.Join(dir, id+form.ext)
	if err := os.Rename(form.path, stored); err != nil {
		form.cleanup()
		log.Printf("upload %s: rename: %v", id, err)
		writeError(w, http.StatusInternalServerError, "save-failed")
		return
	}
	defer os.Remove(stored)
	u := &Upload{ID: id, User: user, Filename: form.filename, CreatedAt: time.Now().UnixMilli()}
	status, body := a.placeUpload(r.Context(), u, form, stored)
	if err := a.store.SaveUpload(u); err != nil {
		log.Printf("upload %s: db: %v", id, err)
	}
	writeJSON(w, status, body)
}

func (a *App) placeUpload(ctx context.Context, u *Upload, form *uploadForm, stored string) (int, any) {
	fail := func(status int, code string) (int, any) {
		u.Status = "rejected"
		return status, map[string]string{"error": code}
	}
	tags, err := probe(ctx, stored)
	if errors.Is(err, errNotAudio) {
		return fail(http.StatusUnsupportedMediaType, "unsupported-type")
	}
	if err != nil {
		log.Printf("upload %s: %v", u.ID, err)
		return fail(http.StatusUnprocessableEntity, "probe-failed")
	}
	typed, err := parseTyped(form.tagsJSON)
	if err != nil {
		return fail(http.StatusBadRequest, "bad-tags")
	}
	fields := mergeTags(tags, typed)
	u.Title, u.Artist, u.Album, u.TrackNo = fields["title"], fields["artist"], fields["album"], number(fields["track"])
	dir := a.albumDir(u.User, fields)
	hasCover := tags.Cover || form.cover != nil || (fields["album"] != "" && fields["artist"] != "" && hasCoverFile(dir))
	if missing := missingTags(fields, hasCover); len(missing) > 0 {
		u.Status, u.Missing = "rejected", missing
		return http.StatusUnprocessableEntity, map[string]any{"error": "missing-tags", "missing": missing, "tags": fields}
	}
	if albumArtist := chooseAlbumArtist(typed, fields, existingAlbumArtist(ctx, dir)); albumArtist != "" {
		if albumArtist != tags.Fields["albumartist"] {
			typed["albumartist"] = albumArtist
		}
		fields["albumartist"] = albumArtist
	}
	src := stored
	if len(typed) > 0 {
		tagged := strings.TrimSuffix(stored, form.ext) + ".tagged" + form.ext
		defer os.Remove(tagged)
		if err := writeTags(ctx, stored, tagged, form.ext, typed); err != nil {
			log.Printf("upload %s: %v", u.ID, err)
			return fail(http.StatusInternalServerError, "tag-write-failed")
		}
		src = tagged
	}
	if err := os.MkdirAll(dir, 0o775|os.ModeSetgid); err != nil {
		log.Printf("upload %s: mkdir: %v", u.ID, err)
		return fail(http.StatusInternalServerError, "save-failed")
	}
	dst := filepath.Join(dir, trackFileName(fields, form.ext))
	if err := moveNoReplace(src, dst); err != nil {
		if errors.Is(err, errExists) {
			return fail(http.StatusConflict, "exists")
		}
		log.Printf("upload %s: move: %v", u.ID, err)
		return fail(http.StatusInternalServerError, "save-failed")
	}
	os.Chmod(dst, 0o664)
	var coverErr error
	if form.cover != nil {
		coverErr = replaceCover(ctx, dir, form.cover)
	} else if tags.Cover && !hasCoverFile(dir) {
		coverErr = extractCover(ctx, dst, filepath.Join(dir, "cover.jpg"))
	}
	if coverErr != nil && !errors.Is(coverErr, os.ErrExist) {
		log.Printf("upload %s: cover: %v", u.ID, coverErr)
	}
	if form.back != nil {
		if err := saveBackCover(ctx, dir, form.back); err != nil {
			log.Printf("upload %s: back cover: %v", u.ID, err)
		}
	}
	rel, _ := filepath.Rel(a.cfg.LibraryDir(), dst)
	u.Status, u.LibraryPath = "in-library", rel
	if form.vinyl {
		if err := a.SetVinylFolder(filepath.Dir(rel), true); err != nil {
			log.Printf("upload %s: vinyl: %v", u.ID, err)
		}
	}
	a.scanner.Trigger()
	return http.StatusCreated, u
}

func (a *App) handleGetUpload(w http.ResponseWriter, r *http.Request, user string) {
	u, err := a.store.GetUpload(r.PathValue("id"), user)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not-found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *App) handleListUploads(w http.ResponseWriter, r *http.Request, user string) {
	list, err := a.store.RecentUploads(user, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleCoverExists(w http.ResponseWriter, r *http.Request, user string) {
	q := r.URL.Query()
	fields := map[string]string{}
	for _, k := range []string{"artist", "album"} {
		fields[k] = cleanTag(q.Get(k))
	}
	exists := fields["album"] != "" && fields["artist"] != "" && hasCoverFile(a.albumDir(user, fields))
	writeJSON(w, http.StatusOK, map[string]bool{"exists": exists})
}
