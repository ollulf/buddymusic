package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	store, err := OpenStore(filepath.Join(cfg.DataDir, "buddymusic.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()
	store.PurgeStats(time.Now().Add(-statsRetention))

	app := NewApp(cfg, store)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	if _, err := os.Stat(cfg.FFmpeg); err == nil {
		go app.transcodes.Run(ctx)
	} else {
		log.Printf("transcode: %s not found, songs are transcoded live", cfg.FFmpeg)
	}

	log.Printf("buddymusic-api listening on %s", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

var version = "dev"
