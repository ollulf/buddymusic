package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
)

type App struct {
	cfg     *Config
	store   *Store
	nav     *Navidrome
	limiter loginLimiter
	scanner *Scanner
}

func NewApp(cfg *Config, store *Store) *App {
	nav := NewNavidrome(cfg.NavidromeURL)
	return &App{cfg: cfg, store: store, nav: nav, scanner: NewScanner(nav, cfg.ScanUser, cfg.ScanPassword, cfg.ScanDelay)}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.Handle("GET /api/me", a.requireUser(a.handleMe))
	mux.Handle("POST /api/uploads", a.requireUser(a.handleUpload))
	mux.Handle("GET /api/uploads", a.requireUser(a.handleListUploads))
	mux.Handle("GET /api/uploads/{id}", a.requireUser(a.handleGetUpload))
	mux.Handle("GET /api/covers", a.requireUser(a.handleCoverExists))
	mux.Handle("GET /api/music/artists", a.requireUser(a.musicJSON("getArtists.view", "artists", noParams)))
	mux.Handle("GET /api/music/artist/{id}", a.requireUser(a.musicJSON("getArtist.view", "artist", idParam)))
	mux.Handle("GET /api/music/albums", a.requireUser(a.musicJSON("getAlbumList2.view", "albumList2", albumListParams)))
	mux.Handle("GET /api/music/album/{id}", a.requireUser(a.musicJSON("getAlbum.view", "album", idParam)))
	mux.Handle("GET /api/music/search", a.requireUser(a.musicJSON("search3.view", "searchResult3", searchParams)))
	mux.Handle("GET /api/music/cover/{id}", a.requireUser(a.handleCover))
	mux.Handle("GET /api/music/stream/{id}", a.requireUser(a.handleStream))
	mux.Handle("POST /api/music/scrobble/{id}", a.requireUser(a.handleScrobble))
	mux.Handle("GET /", a.webHandler())
	return a.trustedOnly(mux)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type userHandler func(w http.ResponseWriter, r *http.Request, user string)

func (a *App) trustedOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		addr = addr.Unmap()
		for _, p := range a.cfg.TrustedNets {
			if p.Contains(addr) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
