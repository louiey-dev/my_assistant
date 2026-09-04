// Package web serves the compiled dashboard embedded in the backend binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The release build refreshes this directory from frontend/dist before compiling.
// Keeping the directory in the repository also lets local Go builds compile.
//
//go:embed dist
var dashboard embed.FS

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	files, err := fs.Sub(dashboard, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/" {
			// Serve the SPA entrypoint directly. FileServer can redirect a
			// directory request to "./" for some FS implementations.
			index, readErr := fs.ReadFile(files, "index.html")
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || !strings.Contains(path.Base(name), ".") {
			r.URL.Path = "/index.html"
		}
		server.ServeHTTP(w, r)
	})
}
