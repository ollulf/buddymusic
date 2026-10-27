package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const pushSchema = `
CREATE TABLE IF NOT EXISTS push_vapid (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	priv BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subs (
	endpoint TEXT PRIMARY KEY,
	user TEXT NOT NULL,
	p256dh TEXT NOT NULL,
	auth TEXT NOT NULL,
	lang TEXT NOT NULL DEFAULT 'en',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS push_subs_user ON push_subs(user);`

const (
	pushSubject     = "https://github.com/ollulf/buddymusic"
	pushTTL         = 24 * 60 * 60
	pushMaxPerUser  = 10
	pushEndpointMax = 1024
)

var b64 = base64.RawURLEncoding

type PushSub struct {
	Endpoint string
	P256dh   string
	Auth     string
	Lang     string
}

type pusher struct {
	key    *ecdsa.PrivateKey
	pub    []byte
	client *http.Client
	wg     sync.WaitGroup
}

func (s *Store) VAPIDKey() (*ecdsa.PrivateKey, error) {
	var der []byte
	err := s.db.QueryRow(`SELECT priv FROM push_vapid WHERE id = 1`).Scan(&der)
	if errors.Is(err, sql.ErrNoRows) {
		key, gerr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if gerr != nil {
			return nil, gerr
		}
		fresh, merr := x509.MarshalECPrivateKey(key)
		if merr != nil {
			return nil, merr
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO push_vapid (id, priv) VALUES (1, ?)`, fresh); err != nil {
			return nil, err
		}
		err = s.db.QueryRow(`SELECT priv FROM push_vapid WHERE id = 1`).Scan(&der)
	}
	if err != nil {
		return nil, err
	}
	return x509.ParseECPrivateKey(der)
}

func (s *Store) SavePushSub(user string, sub PushSub, now time.Time) error {
	_, err := s.db.Exec(`INSERT INTO push_subs (endpoint, user, p256dh, auth, lang, created_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET user=excluded.user, p256dh=excluded.p256dh, auth=excluded.auth, lang=excluded.lang`,
		sub.Endpoint, user, sub.P256dh, sub.Auth, sub.Lang, now.Unix())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM push_subs WHERE user = ? AND endpoint NOT IN
(SELECT endpoint FROM push_subs WHERE user = ? ORDER BY created_at DESC, rowid DESC LIMIT ?)`, user, user, pushMaxPerUser)
	return err
}

func (s *Store) DeletePushSub(user, endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subs WHERE user = ? AND endpoint = ?`, user, endpoint)
	return err
}

func (s *Store) DropPushEndpoint(endpoint string) {
	s.db.Exec(`DELETE FROM push_subs WHERE endpoint = ?`, endpoint)
}

func (s *Store) PushSubs(user string) ([]PushSub, error) {
	out := []PushSub{}
	rows, err := s.db.Query(`SELECT endpoint, p256dh, auth, lang FROM push_subs WHERE user = ?`, user)
	err = scanEach(rows, err, func(r *sql.Rows) error {
		var p PushSub
		if err := r.Scan(&p.Endpoint, &p.P256dh, &p.Auth, &p.Lang); err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	return out, err
}

func newPusher(store *Store) *pusher {
	key, err := store.VAPIDKey()
	if err != nil {
		log.Printf("push: vapid key: %v", err)
		return &pusher{client: &http.Client{Timeout: 15 * time.Second}}
	}
	pub, _ := key.PublicKey.ECDH()
	return &pusher{key: key, pub: pub.Bytes(), client: &http.Client{Timeout: 15 * time.Second}}
}

func encryptPush(sub PushSub, plaintext []byte) ([]byte, error) {
	uaPubBytes, err := b64.DecodeString(strings.TrimRight(sub.P256dh, "="))
	if err != nil {
		return nil, err
	}
	authSecret, err := b64.DecodeString(strings.TrimRight(sub.Auth, "="))
	if err != nil || len(authSecret) != 16 {
		return nil, errors.New("bad auth secret")
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, err
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encryptPushWith(uaPub, authSecret, asPriv, salt, plaintext)
}

func pushKeys(shared, authSecret, uaPub, asPub, salt []byte) (cek, nonce []byte, err error) {
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPub...), asPub...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	return cek, nonce, err
}

func encryptPushWith(uaPub *ecdh.PublicKey, authSecret []byte, asPriv *ecdh.PrivateKey, salt, plaintext []byte) ([]byte, error) {
	shared, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes()
	cek, nonce, err := pushKeys(shared, authSecret, uaPub.Bytes(), asPub, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Write(salt)
	binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(byte(len(asPub)))
	out.Write(asPub)
	out.Write(gcm.Seal(nil, nonce, append(append([]byte{}, plaintext...), 2), nil))
	return out.Bytes(), nil
}

func (p *pusher) vapidJWT(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": pushSubject})
	signing := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

var errPushGone = errors.New("subscription gone")

func (p *pusher) send(sub PushSub, payload []byte) error {
	if p.key == nil {
		return errors.New("no vapid key")
	}
	body, err := encryptPush(sub, payload)
	if err != nil {
		return err
	}
	jwt, err := p.vapidJWT(sub.Endpoint, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", fmt.Sprint(pushTTL))
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+b64.EncodeToString(p.pub))
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errPushGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service: %s", resp.Status)
	}
	return nil
}

func recPushText(lang, from, kind string, song RecSong) string {
	what := song.Title
	if kind == recKindAlbum {
		what = song.Album
	}
	if song.Artist != "" {
		what += " – " + song.Artist
	}
	if kind == recKindAlbum {
		if lang == "de" {
			return from + " empfiehlt das Album " + what
		}
		return from + " recommends the album " + what
	}
	if lang == "de" {
		return from + " empfiehlt " + what
	}
	return from + " recommends " + what
}

func (a *App) notifyRecs(from string, to []string, kind string, song RecSong, note string) {
	a.push.wg.Add(1)
	go func() {
		defer a.push.wg.Done()
		for _, user := range to {
			subs, err := a.store.PushSubs(user)
			if err != nil {
				log.Printf("push: subs: %v", err)
				continue
			}
			for _, sub := range subs {
				payload, _ := json.Marshal(map[string]string{"title": recPushText(sub.Lang, from, kind, song), "body": note, "url": "/?recs=1", "tag": "rec"})
				err := a.push.send(sub, payload)
				if errors.Is(err, errPushGone) {
					a.store.DropPushEndpoint(sub.Endpoint)
				} else if err != nil {
					log.Printf("push: send: %v", err)
				}
			}
		}
	}()
}

func (a *App) handlePushKey(w http.ResponseWriter, r *http.Request, user string) {
	if a.push.key == nil {
		writeError(w, http.StatusServiceUnavailable, "no-key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": b64.EncodeToString(a.push.pub)})
}

func validPushEndpoint(s string) bool {
	if len(s) > pushEndpointMax {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func (a *App) handlePushSubscribe(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Lang string `json:"lang"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	pub, err1 := b64.DecodeString(strings.TrimRight(body.Keys.P256dh, "="))
	auth, err2 := b64.DecodeString(strings.TrimRight(body.Keys.Auth, "="))
	if !validPushEndpoint(body.Endpoint) || err1 != nil || err2 != nil || len(pub) != 65 || len(auth) != 16 {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	lang := "en"
	if body.Lang == "de" {
		lang = "de"
	}
	sub := PushSub{Endpoint: body.Endpoint, P256dh: b64.EncodeToString(pub), Auth: b64.EncodeToString(auth), Lang: lang}
	if err := a.store.SavePushSub(user, sub, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request, user string) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := a.store.DeletePushSub(user, body.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
