package main

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
)

//go:embed web
var webFS embed.FS

const shellPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<title>buddymusic</title>
<link rel="stylesheet" href="/app.css">
<script src="/vendor/jsmediatags.min.js" defer></script>
<script type="module" src="/app.js"></script>
</head>
<body><div id="app"></div></body>
</html>
`

func init() {
	mime.AddExtensionType(".ttf", "font/ttf")
}

func (a *App) webHandler() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'")
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write([]byte(shellPage))
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
