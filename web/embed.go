package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
)

//go:embed static/*
var staticFiles embed.FS

func sub() fs.FS {
	if fi, err := os.Stat("web/static"); err == nil && fi.IsDir() {
		return os.DirFS("web/static")
	}
	s, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("failed to get static sub-filesystem: " + err.Error())
	}
	return s
}

// StaticHandler serves the dashboard assets.
func StaticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		http.FileServer(http.FS(sub())).ServeHTTP(w, r)
	})
}

// GetFaviconBytes returns the raw bytes of the embedded favicon.png.
func GetFaviconBytes() []byte {
	b, err := staticFiles.ReadFile("static/favicon.png")
	if err != nil {
		return nil
	}
	return b
}


// Page serves one embedded HTML file (used for / , /login and /setup).
// Pages are never cached so a new release is picked up immediately.
func Page(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(sub(), name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
}
