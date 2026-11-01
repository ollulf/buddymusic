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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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
	sizes   map[int64][]storeFile
	files   []storeFile
	indexMu sync.Mutex
}

type storeFile struct {
	path  string
	rel   string
	info  fs.FileInfo
	name  string
	album string
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
	return t.find(size, "")
}

func (t *Transcodes) find(size int64, navPath string) (string, bool) {
	t.mu.Lock()
	built := t.sizes != nil
	t.mu.Unlock()
	if !built {
		t.Index(context.Background())
	}
	name, album := songName(navPath), songAlbum(navPath)
	t.mu.Lock()
	defer t.mu.Unlock()
	if size > 0 {
		cands := t.sizes[size]
		if len(cands) == 1 {
			return cands[0].path, true
		}
		if p, ok := pickByName(cands, name, album); ok {
			return p, true
		}
	}
	if name == "" || album == "" {
		return "", false
	}
	var same []storeFile
	for _, f := range t.files {
		if f.name == name && f.album == album {
			same = append(same, f)
		}
	}
	if len(same) == 1 {
		return same[0].path, true
	}
	return "", false
}

func pickByName(cands []storeFile, name, album string) (string, bool) {
	if name == "" || len(cands) == 0 {
		return "", false
	}
	var byName, byBoth []string
	for _, c := range cands {
		if c.name != name {
			continue
		}
		byName = append(byName, c.path)
		if album != "" && c.album == album {
			byBoth = append(byBoth, c.path)
		}
	}
	if len(byBoth) == 1 {
		return byBoth[0], true
	}
	if len(byName) == 1 {
		return byName[0], true
	}
	return "", false
}

var trackPrefix = regexp.MustCompile(`^\s*\d{1,3}(\s*[-.]\s*\d{1,3})?\s*[-._)]?\s+`)

func songName(p string) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(filepath.FromSlash(p))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if stripped := trackPrefix.ReplaceAllString(base, ""); stripped != "" {
		base = stripped
	}
	return foldName(base)
}

func songAlbum(p string) string {
	if p == "" {
		return ""
	}
	dir := filepath.Dir(filepath.FromSlash(p))
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return foldName(filepath.Base(dir))
}

func foldName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (t *Transcodes) scan(ctx context.Context) ([]storeFile, error) {
	var files []storeFile
	err := filepath.WalkDir(t.lib, func(p string, d fs.DirEntry, err error) error {
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
		rel = filepath.ToSlash(rel)
		files = append(files, storeFile{path: p, rel: rel, info: info, name: songName(rel), album: songAlbum(rel)})
		return nil
	})
	return files, err
}

func (t *Transcodes) publish(files []storeFile) {
	sizes := map[int64][]storeFile{}
	for _, f := range files {
		sizes[f.info.Size()] = append(sizes[f.info.Size()], f)
	}
	t.mu.Lock()
	t.sizes = sizes
	t.files = files
	t.mu.Unlock()
}

func (t *Transcodes) Index(ctx context.Context) ([]storeFile, error) {
	t.indexMu.Lock()
	defer t.indexMu.Unlock()
	files, err := t.scan(ctx)
	if err != nil {
		return nil, err
	}
	t.publish(files)
	return files, nil
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
	go t.Index(context.Background())
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
	start := time.Now()
	files, err := t.Index(ctx)
	if err != nil {
		return made
	}
	keep := map[string]bool{}
	for _, f := range files {
		key := transcodeKey(f.rel, f.info)
		for _, tier := range transcodeTiers {
			keep[t.target(key, tier)] = true
		}
	}
	for _, f := range files {
		key := transcodeKey(f.rel, f.info)
		for _, tier := range transcodeTiers {
			if ctx.Err() != nil {
				return made
			}
			dst := t.target(key, tier)
			if ready(dst) {
				continue
			}
			t.mu.Lock()
			failed := t.failed[dst]
			t.mu.Unlock()
			if failed {
				continue
			}
			fl := t.flight(f.path, dst, tier, t.encode)
			select {
			case <-ctx.Done():
				return made
			case <-fl.done:
			}
			if fl.err != nil {
				log.Printf("transcode %s (%s %d): %v", f.rel, tier.Format, tier.Kbps, fl.err)
				continue
			}
			made++
		}
	}
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
