package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"time"
)

type App struct {
	cfg        *Config
	store      *Store
	nav        *Navidrome
	limiter    loginLimiter
	scanner    *Scanner
	admins     adminCache
	seen       seenThrottle
	jumps      jumpLimiter
	feedback   jumpLimiter
	feat       featuredCache
	push       *pusher
	vinylTry   vinylThrottle
	transcodes *Transcodes
}

func NewApp(cfg *Config, store *Store) *App {
	nav := NewNavidrome(cfg.NavidromeURL)
	return &App{cfg: cfg, store: store, nav: nav, scanner: NewScanner(nav, cfg.ScanUser, cfg.ScanPassword, cfg.ScanDelay),
		feedback: jumpLimiter{window: 10 * time.Minute, max: 5}, push: newPusher(store),
		transcodes: NewTranscodes(filepath.Join(cfg.DataDir, "transcodes"), cfg.LibraryDir(), ffmpegEncoder(cfg.FFmpeg))}
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("POST /api/login", a.handleLogin)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.Handle("GET /api/me", a.requireUser(a.handleMe))
	mux.Handle("GET /api/avatar", a.requireUser(a.handleGetAvatar))
	mux.Handle("PUT /api/avatar", a.requireUser(a.handlePutAvatar))
	mux.Handle("DELETE /api/avatar", a.requireUser(a.handleDeleteAvatar))
	mux.Handle("GET /api/avatar/{user}", a.requireUser(a.handleFriendAvatar))
	mux.Handle("GET /api/friends", a.requireUser(a.handleFriends))
	mux.Handle("GET /api/recs", a.requireUser(a.handleListRecs))
	mux.Handle("POST /api/recs", a.requireUser(a.handleSendRec))
	mux.Handle("POST /api/recs/{id}", a.requireUser(a.handleMarkRec))
	mux.Handle("GET /api/push/key", a.requireUser(a.handlePushKey))
	mux.Handle("POST /api/push/subscription", a.requireUser(a.handlePushSubscribe))
	mux.Handle("DELETE /api/push/subscription", a.requireUser(a.handlePushUnsubscribe))
	mux.Handle("POST /api/uploads", a.requireUser(a.handleUpload))
	mux.Handle("GET /api/uploads", a.requireUser(a.handleListUploads))
	mux.Handle("GET /api/uploads/{id}", a.requireUser(a.handleGetUpload))
	mux.Handle("GET /api/covers", a.requireUser(a.handleCoverExists))
	mux.Handle("GET /api/music/artists", a.requireUser(a.handleArtists))
	mux.Handle("GET /api/music/artist/{id}", a.requireUser(a.handleArtist))
	mux.Handle("GET /api/music/albums", a.requireUser(a.handleAlbums))
	mux.Handle("GET /api/music/genres", a.requireUser(a.handleGenres))
	mux.Handle("GET /api/music/album/{id}", a.requireUser(a.handleAlbum))
	mux.Handle("POST /api/music/album/{id}/opened", a.requireUser(a.handleAlbumOpened))
	mux.Handle("GET /api/music/random-songs", a.requireUser(a.musicJSON("getRandomSongs.view", "randomSongs", randomSongsParams)))
	mux.Handle("GET /api/music/search", a.requireUser(a.musicJSON("search3.view", "searchResult3", searchParams)))
	mux.Handle("GET /api/music/starred", a.requireUser(a.musicJSON("getStarred2.view", "starred2", noParams)))
	mux.Handle("POST /api/music/star/{id}", a.requireUser(a.handleStar(true)))
	mux.Handle("DELETE /api/music/star/{id}", a.requireUser(a.handleStar(false)))
	mux.Handle("GET /api/music/playlists", a.requireUser(a.handlePlaylists))
	mux.Handle("POST /api/music/playlists", a.requireUser(a.handlePlaylistCreate))
	mux.Handle("GET /api/music/playlist/{id}", a.requireUser(a.musicJSON("getPlaylist.view", "playlist", idParam)))
	mux.Handle("PATCH /api/music/playlist/{id}", a.requireUser(a.handlePlaylistUpdate))
	mux.Handle("DELETE /api/music/playlist/{id}", a.requireUser(a.handlePlaylistDelete))
	mux.Handle("PUT /api/music/playlist/{id}/songs", a.requireUser(a.handlePlaylistSongs(true)))
	mux.Handle("POST /api/music/playlist/{id}/songs", a.requireUser(a.handlePlaylistSongs(false)))
	mux.Handle("GET /api/music/cover/{id}", a.requireUser(a.handleCover))
	mux.Handle("GET /api/music/back/{id}", a.requireUser(a.handleBackCover))
	mux.Handle("GET /api/music/stream/{id}", a.requireUser(a.handleStream))
	mux.Handle("POST /api/music/scrobble/{id}", a.requireUser(a.handleScrobble))
	mux.Handle("POST /api/music/progress/{id}", a.requireUser(a.handleProgress))
	mux.Handle("POST /api/music/jumplog", a.requireUser(a.handleJumpLog))
	mux.Handle("POST /api/feedback", a.requireUser(a.handleFeedback))
	mux.Handle("GET /api/admin/stats", a.requireUser(a.handleAdminStats))
	mux.Handle("GET /api/admin/feedback", a.requireUser(a.handleAdminFeedback))
	mux.Handle("POST /api/admin/album/{id}/renew", a.requireUser(a.handleAlbumRenew))
	mux.Handle("PATCH /api/admin/album/{id}", a.requireUser(a.handleAlbumEdit))
	mux.Handle("PUT /api/admin/album/{id}/back", a.requireUser(a.handleAlbumBackPut))
	mux.Handle("DELETE /api/admin/album/{id}/back", a.requireUser(a.handleAlbumBackDelete))
	mux.Handle("DELETE /api/music/track/{id}", a.requireUser(a.adminDelete(a.loadTrack)))
	mux.Handle("DELETE /api/music/album/{id}", a.requireUser(a.adminDelete(a.loadAlbum)))
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
