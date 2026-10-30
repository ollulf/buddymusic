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
	dir    string
	lib    string
	encode encodeFunc
	kick   chan struct{}
	every  time.Duration
	delay  time.Duration
	mu     sync.Mutex
	failed map[string]bool
	ids    map[string]songPathEntry
}

type songPathEntry struct {
	path string
	at   time.Time
}

func NewTranscodes(dir, lib string, enc encodeFunc) *Transcodes {
	return &Transcodes{dir: dir, lib: lib, encode: enc, kick: make(chan struct{}, 1), every: 6 * time.Hour, delay: 2 * time.Minute,
		failed: map[string]bool{}, ids: map[string]songPathEntry{}}
}

func ffmpegEncoder(bin string) encodeFunc {
	nice, _ := exec.LookPath("nice")
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

func (t *Transcodes) Lookup(src string, tier transcodeTier) (string, bool) {
	rel, err := filepath.Rel(t.lib, src)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	info, err := os.Stat(src)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	p := t.target(transcodeKey(filepath.ToSlash(rel), info), tier)
	if st, err := os.Stat(p); err != nil || st.Size() == 0 {
		return "", false
	}
	return p, true
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
		for _, tier := range transcodeTiers {
			dst := t.target(key, tier)
			keep[dst] = true
			if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
				continue
			}
			if t.failed[dst] {
				continue
			}
			if err := t.encodeOne(ctx, p, dst, tier); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				t.failed[dst] = true
				log.Printf("transcode %s (%s %d): %v", rel, tier.Format, tier.Kbps, err)
				continue
			}
			made++
		}
		return nil
	})
	if walkErr != nil {
		return made
	}
	removed := 0
	filepath.WalkDir(t.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !keep[p] {
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

func (t *Transcodes) encodeOne(ctx context.Context, src, dst string, tier transcodeTier) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := strings.TrimSuffix(dst, tier.Ext) + ".tmp" + tier.Ext
	defer os.Remove(tmp)
	if err := t.encode(ctx, src, tmp, tier); err != nil {
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
