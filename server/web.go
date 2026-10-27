package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
)

//go:embed web
var webFS embed.FS

const shellHead = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<title>buddymusic</title>
<link rel="icon" href="/icon.svg" type="image/svg+xml">
<link rel="icon" href="/icon-32.png" type="image/png" sizes="32x32">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<link rel="manifest" href="/manifest.webmanifest">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="theme-color" content="#1a1c2c">
<script src="/theme.js"></script>
<link rel="stylesheet" href="/app.css">
<script src="/vendor/jsmediatags.min.js" defer></script>
<script type="module" src="/app.js"></script>
`

const shellTail = `</head>
<body><div id="app"></div></body>
</html>
`

func init() {
	mime.AddExtensionType(".ttf", "font/ttf")
	mime.AddExtensionType(".woff2", "font/woff2")
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type staticFile struct {
	body  []byte
	gz    []byte
	ctype string
	etag  string
	cache string
}

func buildShell(modules []string) []byte {
	var b strings.Builder
	b.WriteString(shellHead)
	for _, m := range modules {
		b.WriteString(`<link rel="modulepreload" href="/` + m + `">` + "\n")
	}
	b.WriteString(shellTail)
	return []byte(b.String())
}

func newStaticFile(body []byte, ctype, cache string) *staticFile {
	sum := sha256.Sum256(body)
	f := &staticFile{body: body, ctype: ctype, etag: `"` + hex.EncodeToString(sum[:8]) + `"`, cache: cache}
	if compressible(ctype) && len(body) > 512 {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(body)
		zw.Close()
		if buf.Len() < len(body) {
			f.gz = buf.Bytes()
		}
	}
	return f
}

func compressible(ctype string) bool {
	return strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "javascript") || strings.Contains(ctype, "json") || strings.Contains(ctype, "svg")
}

func loadStatic() map[string]*staticFile {
	sub, _ := fs.Sub(webFS, "web")
	files := map[string]*staticFile{}
	var modules []string
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		ctype := mime.TypeByExtension(path.Ext(p))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		cache := "no-cache"
		if strings.HasPrefix(p, "vendor/") {
			cache = "public, max-age=604800"
		}
		files["/"+p] = newStaticFile(body, ctype, cache)
		if strings.HasSuffix(p, ".js") && !strings.HasPrefix(p, "vendor/") && p != "app.js" && p != "theme.js" && p != "sw.js" {
			modules = append(modules, p)
		}
		return nil
	})
	sort.Strings(modules)
	files["/"] = newStaticFile(buildShell(modules), "text/html; charset=utf-8", "no-cache")
	return files
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.ReplaceAll(q, " ", "") != "q=0" {
			return true
		}
	}
	return false
}

func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == etag {
			return true
		}
	}
	return false
}

func (a *App) webHandler() http.Handler {
	files := loadStatic()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'")
		f, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h.Set("Content-Type", f.ctype)
		h.Set("Cache-Control", f.cache)
		h.Set("ETag", f.etag)
		if f.gz != nil {
			h.Set("Vary", "Accept-Encoding")
		}
		if etagMatches(r.Header.Get("If-None-Match"), f.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := f.body
		if f.gz != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			body = f.gz
		}
		w.Write(body)
	})
}
