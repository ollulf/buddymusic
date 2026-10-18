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
}

func NewApp(cfg *Config, store *Store) *App {
	return &App{cfg: cfg, store: store, nav: NewNavidrome(cfg.NavidromeURL)}
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
