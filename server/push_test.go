package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeUA struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newFakeUA(t *testing.T) *fakeUA {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &fakeUA{priv: priv, auth: auth}
}

func (u *fakeUA) sub(endpoint string) PushSub {
	return PushSub{Endpoint: endpoint, P256dh: b64.EncodeToString(u.priv.PublicKey().Bytes()), Auth: b64.EncodeToString(u.auth), Lang: "en"}
}

func (u *fakeUA) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("short body")
	}
	salt := body[:16]
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != 4096 {
		t.Fatalf("rs %d", rs)
	}
	idlen := int(body[20])
	asPubBytes := body[21 : 21+idlen]
	asPub, err := ecdh.P256().NewPublicKey(asPubBytes)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := u.priv.ECDH(asPub)
	if err != nil {
		t.Fatal(err)
	}
	cek, nonce, err := pushKeys(shared, u.auth, u.priv.PublicKey().Bytes(), asPubBytes, salt)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain[len(plain)-1] != 2 {
		t.Fatalf("missing delimiter")
	}
	return plain[:len(plain)-1]
}

func TestVAPIDKeyPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	k1, err := s.VAPIDKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := s.VAPIDKey()
	s.Close()
	s, _ = OpenStore(path)
	defer s.Close()
	k3, err := s.VAPIDKey()
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Equal(k2) || !k1.Equal(k3) {
		t.Fatal("vapid key changed")
	}
}

func TestPushEncryptRoundTrip(t *testing.T) {
	ua := newFakeUA(t)
	msg := []byte(`{"title":"hi"}`)
	body, err := encryptPush(ua.sub("https://push.example/x"), msg)
	if err != nil {
		t.Fatal(err)
	}
	if got := ua.decrypt(t, body); string(got) != string(msg) {
		t.Fatalf("got %q", got)
	}
}

func TestVAPIDJWTVerifies(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "p.db"))
	defer s.Close()
	p := newPusher(s)
	jwt, err := p.vapidJWT("https://push.example:8443/abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	claims, _ := b64.DecodeString(parts[1])
	if !strings.Contains(string(claims), `"aud":"https://push.example:8443"`) {
		t.Fatalf("claims %s", claims)
	}
	sig, _ := b64.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&p.key.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("bad signature")
	}
}

func TestPushSubscriptionEndpoints(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(max)
		return do(t, a.Routes(), req)
	}
	rec := call("GET", "/api/push/key", "")
	var key struct{ Key string }
	json.Unmarshal(rec.Body.Bytes(), &key)
	if raw, _ := b64.DecodeString(key.Key); rec.Code != 200 || len(raw) != 65 {
		t.Fatalf("key: %d %s", rec.Code, rec.Body)
	}
	ua := newFakeUA(t)
	sub := ua.sub("https://push.example/one")
	good := func(endpoint string) string {
		b, _ := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": sub.P256dh, "auth": sub.Auth}, "lang": "de"})
		return string(b)
	}
	if rec := call("POST", "/api/push/subscription", good("http://push.example/x")); rec.Code != 400 {
		t.Fatalf("http endpoint: %d", rec.Code)
	}
	if rec := call("POST", "/api/push/subscription", `{"endpoint":"https://push.example/x","keys":{"p256dh":"AAAA","auth":"AAAA"}}`); rec.Code != 400 {
		t.Fatalf("bad keys: %d", rec.Code)
	}
	for _, e := range []string{"https://push.example/one", "https://push.example/two", "https://push.example/one"} {
		if rec := call("POST", "/api/push/subscription", good(e)); rec.Code != 200 {
			t.Fatalf("subscribe: %d %s", rec.Code, rec.Body)
		}
	}
	subs, _ := a.store.PushSubs("max")
	if len(subs) != 2 || subs[0].Lang != "de" {
		t.Fatalf("subs: %+v", subs)
	}
	if rec := call("DELETE", "/api/push/subscription", `{"endpoint":"https://push.example/one"}`); rec.Code != 200 {
		t.Fatalf("unsubscribe: %d", rec.Code)
	}
	if subs, _ := a.store.PushSubs("max"); len(subs) != 1 || subs[0].Endpoint != "https://push.example/two" {
		t.Fatalf("after delete: %+v", subs)
	}
}

func TestPushSentOnRec(t *testing.T) {
	a := newTestApp(t)
	max := login(t, a)
	boss := loginAs(t, a, "boss", "bosspw")
	ua := newFakeUA(t)
	var mu sync.Mutex
	var got []map[string]string
	var auths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusGone)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]string
		json.Unmarshal(ua.decrypt(t, body), &m)
		mu.Lock()
		got = append(got, m)
		auths = append(auths, r.Header.Get("Authorization")+"|"+r.Header.Get("Content-Encoding"))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	a.push.client = srv.Client()

	bossName := func() string {
		req := httptest.NewRequest("GET", "/api/me", nil)
		req.AddCookie(boss)
		var me struct{ User string }
		json.Unmarshal(do(t, a.Routes(), req).Body.Bytes(), &me)
		return me.User
	}()
	a.store.SavePushSub(bossName, ua.sub(srv.URL+"/ok"), time.Now())
	a.store.SavePushSub(bossName, ua.sub(srv.URL+"/gone"), time.Now())

	b, _ := json.Marshal(map[string]any{"song": map[string]any{"id": "s1", "title": "Song", "artist": "Band"}, "to": []string{bossName}, "note": "listen"})
	req := httptest.NewRequest("POST", "/api/recs", strings.NewReader(string(b)))
	req.AddCookie(max)
	if rec := do(t, a.Routes(), req); rec.Code != 200 {
		t.Fatalf("send: %d %s", rec.Code, rec.Body)
	}
	a.push.wg.Wait()
	if len(got) != 1 || !strings.HasSuffix(got[0]["title"], "recommends Song – Band") || got[0]["body"] != "listen" || got[0]["url"] != "/?recs=1" {
		t.Fatalf("got %+v", got)
	}
	if !strings.HasPrefix(auths[0], "vapid t=") || !strings.HasSuffix(auths[0], "|aes128gcm") {
		t.Fatalf("headers %s", auths[0])
	}
	if subs, _ := a.store.PushSubs(bossName); len(subs) != 1 || !strings.HasSuffix(subs[0].Endpoint, "/ok") {
		t.Fatalf("gone sub kept: %+v", subs)
	}
}

func TestPushWebFiles(t *testing.T) {
	a := newTestApp(t)
	get := func(p string) *httptest.ResponseRecorder {
		return do(t, a.Routes(), httptest.NewRequest("GET", p, nil))
	}
	if rec := get("/manifest.webmanifest"); rec.Code != 200 || rec.Header().Get("Content-Type") != "application/manifest+json" || !strings.Contains(rec.Body.String(), `"standalone"`) {
		t.Fatalf("manifest: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/sw.js"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "notificationclick") {
		t.Fatalf("sw: %d", rec.Code)
	}
	body := get("/").Body.String()
	if !strings.Contains(body, `rel="manifest" href="/manifest.webmanifest"`) || strings.Contains(body, `href="/sw.js"`) {
		t.Fatalf("shell: %s", body)
	}
}
