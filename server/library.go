package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var coverNames = []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png"}

func (a *App) albumDir(user string, f map[string]string) string {
	return filepath.Join(a.cfg.LibraryDir(), safeSegment(user), safeSegment(f["artist"]), safeSegment(f["album"]))
}

func trackFileName(f map[string]string, ext string) string {
	name := fmt.Sprintf("%02d - %s", number(f["track"]), f["title"])
	if disc := number(f["disc"]); disc > 1 {
		name = fmt.Sprintf("%d-%s", disc, name)
	}
	return safeSegment(name) + ext
}

func hasCoverFile(dir string) bool {
	for _, n := range coverNames {
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return true
		}
	}
	return false
}

var errExists = errors.New("exists")

func moveNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errExists
		}
		if err := copyNoReplace(src, dst); err != nil {
			return err
		}
	}
	return os.Remove(src)
}

func copyNoReplace(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errExists
		}
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

func replaceCover(ctx context.Context, dir string, data []byte) error {
	data = shrinkCover(ctx, dir, data)
	ext := sniffImage(data)
	if ext == "" {
		return errors.New("unsupported cover type")
	}
	tmp, err := os.CreateTemp(dir, ".cover-*"+ext)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	os.Chmod(tmp.Name(), 0o664)
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "cover"+ext)); err != nil {
		return err
	}
	for _, other := range []string{"cover.jpg", "cover.png"} {
		if other != "cover"+ext {
			os.Remove(filepath.Join(dir, other))
		}
	}
	return nil
}

type Scanner struct {
	nav   *Navidrome
	user  string
	pass  string
	delay time.Duration
	mu    sync.Mutex
	timer *time.Timer
}

func NewScanner(nav *Navidrome, user, pass string, delay time.Duration) *Scanner {
	return &Scanner{nav: nav, user: user, pass: pass, delay: delay}
}

func (s *Scanner) Trigger() {
	if s.pass == "" {
		log.Printf("scan: SCAN_PASSWORD not set, Navidrome picks the files up on its own schedule")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.delay, s.run)
}

func (s *Scanner) run() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.nav.StartScan(ctx, NewCreds(s.user, s.pass)); err != nil {
		log.Printf("scan: %v", err)
		return
	}
	log.Printf("scan: started")
}
