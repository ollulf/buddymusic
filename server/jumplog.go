package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	jumpLogWindow = 10 * time.Minute
	jumpLogMax    = 30
	jumpLogBytes  = 2048
)

type jumpLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *jumpLimiter) allow(user string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	kept := l.hits[user][:0]
	for _, t := range l.hits[user] {
		if now.Sub(t) < jumpLogWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= jumpLogMax {
		l.hits[user] = kept
		return false
	}
	l.hits[user] = append(kept, now)
	return true
}

type jumpReport struct {
	Track      string  `json:"track"`
	Format     string  `json:"format"`
	Kbps       int     `json:"kbps"`
	Quality    string  `json:"quality"`
	From       float64 `json:"from"`
	To         float64 `json:"to"`
	Expected   float64 `json:"expected"`
	Gap        float64 `json:"gap"`
	Visibility string  `json:"visibility"`
	Stalled    bool    `json:"stalled"`
	Buffered   float64 `json:"buffered"`
	Ready      int     `json:"ready"`
	Blob       bool    `json:"blob"`
	Offset     float64 `json:"offset"`
	Agent      string  `json:"agent"`
	Event      string  `json:"event"`
	Detail     string  `json:"detail"`
}

func (a *App) handleJumpLog(w http.ResponseWriter, r *http.Request, user string) {
	var j jumpReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, jumpLogBytes)).Decode(&j); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request")
		return
	}
	if !a.jumps.allow(user, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate-limited")
		return
	}
	log.Printf("jumplog user=%q track=%q fmt=%q kbps=%d quality=%q from=%.2f to=%.2f expected=%.2f gap=%.2f vis=%q stalled=%t buffered=%.2f ready=%d blob=%t offset=%.0f agent=%q event=%q detail=%q",
		clip(user, 64), clip(j.Track, 64), clip(j.Format, 16), j.Kbps, clip(j.Quality, 16), j.From, j.To, j.Expected, j.Gap,
		clip(j.Visibility, 16), j.Stalled, j.Buffered, j.Ready, j.Blob, j.Offset, clip(j.Agent, 32), clip(j.Event, 24), clip(j.Detail, 64))
	w.WriteHeader(http.StatusNoContent)
}
