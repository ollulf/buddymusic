package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var errNoCreds = errors.New("no session credentials")

var ErrForbidden = errors.New("forbidden")

func (a *App) sessionCreds(r *http.Request) (SubsonicCreds, error) {
	var creds SubsonicCreds
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return creds, errNoCreds
	}
	idHash := hashSessionID(c.Value)
	_, sealed, err := a.store.GetSession(idHash)
	if err != nil {
		return creds, err
	}
	plain, err := a.open(sealed, idHash)
	if err != nil {
		return creds, err
	}
	err = json.Unmarshal(plain, &creds)
	return creds, err
}

func (n *Navidrome) query(c SubsonicCreds, extra url.Values) url.Values {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("u", c.User)
	q.Set("t", c.Token)
	q.Set("s", c.Salt)
	q.Set("v", "1.16.1")
	q.Set("c", "buddymusic")
	q.Set("f", "json")
	return q
}

func (n *Navidrome) raw(ctx context.Context, endpoint, key string, c SubsonicCreds, extra url.Values) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", n.base+"/rest/"+endpoint+"?"+n.query(c, extra).Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("navidrome %s: http %d", endpoint, res.StatusCode)
	}
	var out struct {
		Response map[string]json.RawMessage `json:"subsonic-response"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("navidrome %s: %w", endpoint, err)
	}
	var status string
	json.Unmarshal(out.Response["status"], &status)
	if status != "ok" {
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		json.Unmarshal(out.Response["error"], &e)
		if e.Code == 40 || e.Code == 41 {
			return nil, ErrBadLogin
		}
		if e.Code == 70 {
			return nil, ErrNotFound
		}
		if e.Code == 50 {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("navidrome %s: %d %s", endpoint, e.Code, e.Message)
	}
	if v, ok := out.Response[key]; ok {
		return v, nil
	}
	return json.RawMessage("{}"), nil
}

func (a *App) musicJSON(endpoint, key string, params func(r *http.Request) (url.Values, bool)) userHandler {
	return func(w http.ResponseWriter, r *http.Request, user string) {
		q, ok := params(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		creds, err := a.sessionCreds(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		body, err := a.nav.raw(r.Context(), endpoint, key, creds, q)
		switch {
		case errors.Is(err, ErrBadLogin):
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not-found")
			return
		case err != nil:
			log.Printf("music %s: %v", endpoint, err)
			writeError(w, http.StatusBadGateway, "navidrome-unreachable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	}
}

func validID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func noParams(r *http.Request) (url.Values, bool) { return nil, true }

func albumListParams(r *http.Request) (url.Values, bool) {
	t := r.URL.Query().Get("type")
	if t == "" {
		t = "newest"
	}
	switch t {
	case "newest", "recent", "random", "frequent", "alphabeticalByName", "starred":
	default:
		return nil, false
	}
	size := 60
	if s, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && s > 0 && s <= 500 {
		size = s
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	return url.Values{"type": {t}, "size": {strconv.Itoa(size)}, "offset": {strconv.Itoa(offset)}}, true
}

func randomSongsParams(r *http.Request) (url.Values, bool) {
	size := 50
	if v := r.URL.Query().Get("size"); v != "" {
		s, err := strconv.Atoi(v)
		if err != nil || s < 1 || s > 100 {
			return nil, false
		}
		size = s
	}
	return url.Values{"size": {strconv.Itoa(size)}}, true
}

func idParam(r *http.Request) (url.Values, bool) {
	id := r.PathValue("id")
	return url.Values{"id": {id}}, validID(id)
}

func searchParams(r *http.Request) (url.Values, bool) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		return nil, false
	}
	return url.Values{"query": {q}, "artistCount": {"8"}, "albumCount": {"12"}, "songCount": {"30"}}, true
}

var passHeaders = []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"}

var streamClient = &http.Client{Transport: streamTransport()}

func streamTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.MaxIdleConnsPerHost = 16
	return t
}

var streamIdle = 30 * time.Second

var errUpstreamIdle = errors.New("navidrome sent nothing for too long")

func (a *App) proxyBinary(w http.ResponseWriter, r *http.Request, endpoint string, q url.Values, cache string) (sent int64) {
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)
	idle := time.AfterFunc(streamIdle, func() { cancel(errUpstreamIdle) })
	defer idle.Stop()
	req, err := http.NewRequestWithContext(ctx, "GET", a.nav.base+"/rest/"+endpoint+"?"+a.nav.query(creds, q).Encode(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "proxy")
		return
	}
	for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	res, err := streamClient.Do(req)
	idle.Stop()
	if err != nil {
		if r.Context().Err() == nil {
			if errors.Is(context.Cause(ctx), errUpstreamIdle) {
				err = errUpstreamIdle
			}
			log.Printf("music %s: %v", endpoint, err)
		}
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	defer res.Body.Close()
	ct := res.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/xml") || strings.HasPrefix(ct, "application/xml") {
		writeError(w, http.StatusNotFound, "not-found")
		return
	}
	for _, h := range passHeaders {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(res.StatusCode)
	buf := make([]byte, 32<<10)
	flusher, _ := w.(http.Flusher)
	for {
		idle.Reset(streamIdle)
		n, rerr := res.Body.Read(buf)
		idle.Stop()
		if n > 0 {
			m, werr := w.Write(buf[:n])
			sent += int64(m)
			if werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if endpoint != "stream.view" || r.Context().Err() != nil {
				return
			}
			if errors.Is(context.Cause(ctx), errUpstreamIdle) {
				rerr = errUpstreamIdle
			}
			want := res.ContentLength
			if rerr != io.EOF || (want > 0 && sent < want) {
				log.Printf("music stream cut: user=%s track=%s sent=%d want=%d err=%v", creds.User, q.Get("id"), sent, want, rerr)
				panic(http.ErrAbortHandler)
			}
			return
		}
	}
}

func (a *App) handleCover(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	size := 300
	if s, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && s >= 32 && s <= 1200 {
		size = s
	}
	a.proxyBinary(w, r, "getCoverArt.view", url.Values{"id": {id}, "size": {strconv.Itoa(size)}}, "private, max-age=86400")
}

func (a *App) handleStream(w http.ResponseWriter, r *http.Request, user string) {
	id := r.PathValue("id")
	if !validID(id) {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	q, ok := streamParams(id, r.URL.Query())
	if !ok {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	saver := q.Get("format") != ""
	if sent, ok := a.servePretranscoded(w, r, id, q); ok {
		a.recordStream(r, user, id, sent, saver)
		return
	}
	a.recordStream(r, user, id, a.proxyBinary(w, r, "stream.view", q, "private, no-cache"), saver)
}

type countingWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}

func (a *App) servePretranscoded(w http.ResponseWriter, r *http.Request, id string, q url.Values) (int64, bool) {
	if a.transcodes == nil || q.Get("timeOffset") != "" {
		return 0, false
	}
	kbps, _ := strconv.Atoi(q.Get("maxBitRate"))
	tier, ok := findTier(q.Get("format"), kbps)
	if !ok {
		return 0, false
	}
	src, ok := a.transcodes.songPath(id)
	if !ok {
		creds, err := a.sessionCreds(r)
		if err != nil {
			return 0, false
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		songs, err := a.loadTrack(ctx, creds, id)
		cancel()
		if err != nil || len(songs) != 1 {
			return 0, false
		}
		if src, err = a.resolveSongPath(songs[0].Path); err != nil {
			return 0, false
		}
		a.transcodes.rememberSong(id, src)
	}
	p, ok := a.transcodes.Lookup(src, tier)
	if !ok {
		return 0, false
	}
	f, err := os.Open(p)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false
	}
	w.Header().Set("Content-Type", tier.Type)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Pretranscoded", "1")
	cw := &countingWriter{ResponseWriter: w}
	http.ServeContent(cw, r, "", st.ModTime(), f)
	return cw.n, true
}

func streamParams(id string, in url.Values) (url.Values, bool) {
	q := url.Values{"id": {id}}
	format := in.Get("format")
	def := 0
	switch format {
	case "":
		return q, true
	case "opus":
		def = 128
	case "mp3":
		def = 320
	default:
		return nil, false
	}
	rate := def
	if b, err := strconv.Atoi(in.Get("maxBitRate")); err == nil && b >= 32 && b <= 320 {
		rate = b
	}
	q.Set("format", format)
	q.Set("maxBitRate", strconv.Itoa(rate))
	if v := in.Get("timeOffset"); v != "" {
		off, err := strconv.Atoi(v)
		if err != nil || off < 0 || off > 86400 {
			return nil, false
		}
		if off > 0 {
			q.Set("timeOffset", strconv.Itoa(off))
		}
	}
	return q, true
}

func (a *App) handleScrobble(w http.ResponseWriter, r *http.Request, user string) {
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
	sub := "true"
	if r.URL.Query().Get("submission") == "false" {
		sub = "false"
	}
	if _, err := a.nav.raw(r.Context(), "scrobble.view", "", creds, url.Values{"id": {id}, "submission": {sub}}); err != nil {
		log.Printf("scrobble: %v", err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleStar(on bool) userHandler {
	return func(w http.ResponseWriter, r *http.Request, user string) {
		id := r.PathValue("id")
		if !validID(id) {
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		param := "id"
		switch r.URL.Query().Get("kind") {
		case "", "song":
		case "album":
			param = "albumId"
		default:
			writeError(w, http.StatusBadRequest, "bad-request")
			return
		}
		creds, err := a.sessionCreds(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		endpoint := "unstar.view"
		if on {
			endpoint = "star.view"
		}
		_, err = a.nav.raw(r.Context(), endpoint, "", creds, url.Values{param: {id}})
		switch {
		case errors.Is(err, ErrBadLogin):
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not-found")
			return
		case err != nil:
			log.Printf("music %s: %v", endpoint, err)
			writeError(w, http.StatusBadGateway, "navidrome-unreachable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "starred": on})
	}
}
