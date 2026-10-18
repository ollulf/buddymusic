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
	"strconv"
	"strings"
)

var errNoCreds = errors.New("no session credentials")

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

var streamClient = &http.Client{}

func (a *App) proxyBinary(w http.ResponseWriter, r *http.Request, endpoint string, q url.Values, cache string) {
	creds, err := a.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", a.nav.base+"/rest/"+endpoint+"?"+a.nav.query(creds, q).Encode(), nil)
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
	if err != nil {
		if r.Context().Err() == nil {
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
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
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
	q := url.Values{"id": {id}}
	if r.URL.Query().Get("format") == "opus" {
		rate := 128
		if b, err := strconv.Atoi(r.URL.Query().Get("maxBitRate")); err == nil && b >= 32 && b <= 320 {
			rate = b
		}
		q.Set("format", "opus")
		q.Set("maxBitRate", strconv.Itoa(rate))
	}
	a.proxyBinary(w, r, "stream.view", q, "private, no-cache")
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
