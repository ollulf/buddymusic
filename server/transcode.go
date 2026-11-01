package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type transcodeTier struct {
	Format string
	Kbps   int
	Ext    string
	Type   string
	Codec  []string
}

var transcodeTiers = []transcodeTier{
	{Format: "opus", Kbps: 128, Ext: ".opus", Type: "audio/ogg", Codec: []string{"-c:a", "libopus", "-b:a", "128k", "-vbr", "on", "-f", "ogg"}},
	{Format: "mp3", Kbps: 192, Ext: ".mp3", Type: "audio/mpeg", Codec: []string{"-c:a", "libmp3lame", "-b:a", "192k", "-f", "mp3"}},
	{Format: "mp3", Kbps: 128, Ext: ".mp3", Type: "audio/mpeg", Codec: []string{"-c:a", "libmp3lame", "-b:a", "128k", "-f", "mp3"}},
	{Format: "opus", Kbps: 80, Ext: ".opus", Type: "audio/ogg", Codec: []string{"-c:a", "libopus", "-b:a", "80k", "-vbr", "on", "-f", "ogg"}},
}

func findTier(format string, kbps int) (transcodeTier, bool) {
	for _, t := range transcodeTiers {
		if t.Format == format && t.Kbps == kbps {
			return t, true
		}
	}
	return transcodeTier{}, false
}

type encodeFunc func(ctx context.Context, src, dst string, t transcodeTier) error

type Transcodes struct {
	dir     string
	lib     string
	encode  encodeFunc
	urgent  encodeFunc
	wait    time.Duration
	limit   time.Duration
	flights map[string]*encodeFlight
	kick    chan struct{}
	every   time.Duration
	delay   time.Duration
	mu      sync.Mutex
	failed  map[string]bool
	ids     map[string]songPathEntry
	sizes   map[int64][]string
}

type encodeFlight struct {
	done chan struct{}
	err  error
}

type songPathEntry struct {
	path string
	at   time.Time
}

func NewTranscodes(dir, lib string, enc encodeFunc) *Transcodes {
	return &Transcodes{dir: dir, lib: lib, encode: enc, urgent: enc, wait: time.Minute, limit: 10 * time.Minute,
		flights: map[string]*encodeFlight{}, kick: make(chan struct{}, 1), every: 6 * time.Hour, delay: 2 * time.Minute,
		failed: map[string]bool{}, ids: map[string]songPathEntry{}}
}

func ffmpegEncoder(bin string, background bool) encodeFunc {
	nice := ""
	if background {
		nice, _ = exec.LookPath("nice")
	}
	return func(ctx context.Context, src, dst string, t transcodeTier) error {
		args := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", src, "-map", "0:a:0", "-vn", "-map_metadata", "-1"}, t.Codec...)
		args = append(args, dst)
		var cmd *exec.Cmd
		if nice != "" {
			cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "19", bin}, args...)...)
		} else {
			cmd = exec.CommandContext(ctx, bin, args...)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

func transcodeKey(rel string, info fs.FileInfo) string {
	sum := sha256.Sum256([]byte(rel + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\x00" + strconv.FormatInt(info.Size(), 10)))
	return hex.EncodeToString(sum[:16])
}

func (t *Transcodes) target(key string, tier transcodeTier) string {
	return filepath.Join(t.dir, tier.Format+strconv.Itoa(tier.Kbps), key[:2], key+tier.Ext)
}

func (t *Transcodes) relPath(src string) (string, bool) {
	roots := []string{t.lib}
	if real, err := filepath.EvalSymlinks(t.lib); err == nil && real != t.lib {
		roots = append(roots, real)
	}
	for _, root := range roots {
		if inside(root, src) {
			rel, err := filepath.Rel(root, src)
			if err == nil {
				return filepath.ToSlash(rel), true
			}
		}
	}
	return "", false
}

func (t *Transcodes) storePath(src string, tier transcodeTier) (string, error) {
	rel, ok := t.relPath(src)
	if !ok {
		return "", fmt.Errorf("%s is outside the library %s", src, t.lib)
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", src)
	}
	return t.target(transcodeKey(rel, info), tier), nil
}

func ready(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

func (t *Transcodes) Lookup(src string, tier transcodeTier) (string, bool) {
	p, err := t.storePath(src, tier)
	if err != nil || !ready(p) {
		return "", false
	}
	return p, true
}

func (t *Transcodes) Ensure(ctx context.Context, src string, tier transcodeTier) (string, error) {
	p, err := t.storePath(src, tier)
	if err != nil {
		return "", err
	}
	if ready(p) {
		return p, nil
	}
	f := t.flight(src, p, tier, t.urgent)
	wait, cancel := context.WithTimeout(ctx, t.wait)
	defer cancel()
	select {
	case <-f.done:
		if f.err != nil {
			return "", f.err
		}
		return p, nil
	case <-wait.Done():
		return "", fmt.Errorf("encode still running: %w", wait.Err())
	}
}

func (t *Transcodes) flight(src, dst string, tier transcodeTier, enc encodeFunc) *encodeFlight {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f, ok := t.flights[dst]; ok {
		return f
	}
	f := &encodeFlight{done: make(chan struct{})}
	if t.failed[dst] {
		f.err = fmt.Errorf("encode failed before")
		close(f.done)
		return f
	}
	t.flights[dst] = f
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), t.limit)
		defer cancel()
		f.err = t.encodeOne(ctx, src, dst, tier, enc)
		t.mu.Lock()
		delete(t.flights, dst)
		if f.err != nil {
			t.failed[dst] = true
		}
		t.mu.Unlock()
		close(f.done)
	}()
	return f
}

func (t *Transcodes) songPath(id string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.ids[id]
	if !ok || time.Since(e.at) > time.Hour {
		return "", false
	}
	return e.path, true
}

func (t *Transcodes) bySize(size int64) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if size <= 0 || len(t.sizes[size]) != 1 {
		return "", false
	}
	return t.sizes[size][0], true
}

func (t *Transcodes) rememberSong(id, path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.ids) > 20000 {
		t.ids = map[string]songPathEntry{}
	}
	t.ids[id] = songPathEntry{path: path, at: time.Now()}
}

func (t *Transcodes) Kick() {
	if t == nil {
		return
	}
	select {
	case t.kick <- struct{}{}:
	default:
	}
}

func (t *Transcodes) Run(ctx context.Context) {
	tick := time.NewTicker(t.every)
	defer tick.Stop()
	for {
		t.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-t.kick:
			select {
			case <-ctx.Done():
				return
			case <-time.After(t.delay):
			}
		}
	}
}

func (t *Transcodes) Sweep(ctx context.Context) (made int) {
	keep := map[string]bool{}
	sizes := map[int64][]string{}
	start := time.Now()
	walkErr := filepath.WalkDir(t.lib, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != t.lib && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !audioExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(t.lib, p)
		if err != nil {
			return nil
		}
		key := transcodeKey(filepath.ToSlash(rel), info)
		sizes[info.Size()] = append(sizes[info.Size()], p)
		for _, tier := range transcodeTiers {
			dst := t.target(key, tier)
			keep[dst] = true
			if ready(dst) {
				continue
			}
			t.mu.Lock()
			failed := t.failed[dst]
			t.mu.Unlock()
			if failed {
				continue
			}
			f := t.flight(p, dst, tier, t.encode)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.done:
			}
			if f.err != nil {
				log.Printf("transcode %s (%s %d): %v", rel, tier.Format, tier.Kbps, f.err)
				continue
			}
			made++
		}
		return nil
	})
	if walkErr != nil {
		return made
	}
	t.mu.Lock()
	t.sizes = sizes
	t.mu.Unlock()
	removed := 0
	filepath.WalkDir(t.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !keep[p] && !strings.Contains(filepath.Base(p), ".tmp") {
			if os.Remove(p) == nil {
				removed++
			}
		}
		return nil
	})
	if made > 0 || removed > 0 {
		log.Printf("transcode: %d new, %d removed in %s", made, removed, time.Since(start).Round(time.Second))
	}
	return made
}

func (t *Transcodes) encodeOne(ctx context.Context, src, dst string, tier transcodeTier, enc encodeFunc) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := strings.TrimSuffix(dst, tier.Ext) + ".tmp" + tier.Ext
	defer os.Remove(tmp)
	if err := enc(ctx, src, tmp, tier); err != nil {
		return err
	}
	st, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return fmt.Errorf("empty output")
	}
	return os.Rename(tmp, dst)
}
