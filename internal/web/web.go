// Package web serves the embedded reader and isolated article content.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var assets embed.FS

// New serves the embedded app with a fallback for client-side routes.
func New(origin string) http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return newHandler(root, origin)
}

func newHandler(root fs.FS, origin string) http.Handler {
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-src "+origin+"; img-src 'self' "+origin+"; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		info, err := fs.Stat(root, name)
		if err != nil {
			// A missing asset must 404 rather than return the SPA index with a
			// mismatched content type; every other miss is a client-side route.
			if !strings.HasPrefix(name, "assets/") {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		} else if info.IsDir() {
			// A directory such as /assets would otherwise produce a FileServer
			// directory listing of the compiled bundle.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
