package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "bm_session"
	sessionTTL    = 30 * 24 * time.Hour
	shortTTL      = 12 * time.Hour
	loginWindow   = 5 * time.Minute
	loginAttempts = 10
)

type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cut := time.Now().Add(-loginWindow)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= loginAttempts {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, time.Now())
	return true
}

func hashSessionID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func (a *App) seal(plain []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(a.cfg.SessionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plain, []byte(aad)), nil
}

func (a *App) open(sealed []byte, aad string) ([]byte, error) {
	block, err := aes.NewCipher(a.cfg.SessionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("short ciphertext")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(aad))
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-origin")
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.limiter.allow(host) {
		writeError(w, http.StatusTooManyRequests, "too-many-attempts")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember *bool  `json:"remember"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	user := strings.ToLower(strings.TrimSpace(body.Username))
	if user == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "missing-credentials")
		return
	}
	remember := body.Remember == nil || *body.Remember
	ttl := shortTTL
	if remember {
		ttl = sessionTTL
	}
	creds := NewCreds(user, body.Password)
	if err := a.nav.Ping(r.Context(), creds); err != nil {
		if errors.Is(err, ErrBadLogin) {
			a.store.RecordLogin(clip(user, 64), time.Now(), false, deviceName(r.UserAgent()), remember)
			writeError(w, http.StatusUnauthorized, "bad-login")
			return
		}
		log.Printf("login: %v", err)
		writeError(w, http.StatusBadGateway, "navidrome-unreachable")
		return
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	id := base64.RawURLEncoding.EncodeToString(raw)
	idHash := hashSessionID(id)
	plain, _ := json.Marshal(creds)
	sealed, err := a.seal(plain, idHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session")
		return
	}
	if err := a.store.SaveSession(idHash, user, sealed, ttl); err != nil {
		log.Printf("login: save session: %v", err)
		writeError(w, http.StatusInternalServerError, "session")
		return
	}
	a.store.PurgeSessions()
	a.store.RecordLogin(user, time.Now(), true, deviceName(r.UserAgent()), remember)
	a.store.PurgeStats(time.Now().Add(-statsRetention))
	cookie := &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/",
		HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteStrictMode,
	}
	if remember {
		cookie.MaxAge = int(sessionTTL.Seconds())
	}
	http.SetCookie(w, cookie)
	admin, err := a.checkAdmin(r.Context(), creds)
	if err != nil {
		log.Printf("login: admin check: %v", err)
	} else {
		a.admins.put(idHash, admin)
	}
	a.markSeen(r, user)
	writeJSON(w, http.StatusOK, a.meInfo(user, admin))
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.store.DeleteSession(hashSessionID(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, user string) {
	if creds, err := a.sessionCreds(r); err == nil {
		a.resolveVinylAlbums(r.Context(), creds)
	}
	writeJSON(w, http.StatusOK, a.meInfo(user, a.cachedAdmin(r)))
}

func (a *App) meInfo(user string, admin any) map[string]any {
	v, _ := a.store.AvatarVersion(user)
	return map[string]any{"user": user, "admin": admin, "avatar": v,
		"newSince": a.store.NewAlbumsFrom(user, time.Now()), "ownAlbums": a.store.OwnAlbums(user),
		"openedAlbums": a.store.OpenedAlbums(user), "renewedAlbums": a.store.RenewedAlbums(), "vinylAlbums": a.store.VinylAlbums(), "recsUnread": a.store.UnreadRecs(user), "version": version}
}

func (a *App) sessionUser(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return "", ErrNotFound
	}
	user, _, err := a.store.GetSession(hashSessionID(c.Value))
	return user, err
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

func (a *App) requireUser(h userHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-origin")
			return
		}
		user, err := a.sessionUser(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		a.markSeen(r, user)
		h(w, r, user)
	})
}
