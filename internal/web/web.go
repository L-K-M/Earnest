// Package web serves the embedded reader and isolated article content.
package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
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
		if err == nil && !info.IsDir() && strings.HasPrefix(name, "assets/") && serveEncoded(w, r, root, name) {
			return
		}
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

// encodings lists the compressed copies the web build writes next to each
// fingerprinted asset, in order of preference. Serving them avoids sending
// the uncompressed bundle on slow links without compressing per request.
var encodings = []struct{ name, suffix string }{{"br", ".br"}, {"gzip", ".gz"}}

// serveEncoded serves a precompressed copy of name when the client accepts
// one and the build produced it. It reports whether it wrote a response.
func serveEncoded(w http.ResponseWriter, r *http.Request, root fs.FS, name string) bool {
	w.Header().Add("Vary", "Accept-Encoding")
	accept := r.Header.Get("Accept-Encoding")
	for _, encoding := range encodings {
		if !acceptsEncoding(accept, encoding.name) {
			continue
		}
		file, err := root.Open(name + encoding.suffix)
		if err != nil {
			continue
		}
		defer file.Close()
		info, err := file.Stat()
		content, seekable := file.(io.ReadSeeker)
		if err != nil || !seekable {
			return false
		}
		if kind := mime.TypeByExtension(path.Ext(name)); kind != "" {
			w.Header().Set("Content-Type", kind)
		}
		w.Header().Set("Content-Encoding", encoding.name)
		http.ServeContent(w, r, name, info.ModTime(), content)
		return true
	}
	return false
}

// acceptsEncoding reports whether an Accept-Encoding header allows encoding,
// honouring an explicit q=0 refusal.
func acceptsEncoding(header, encoding string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), encoding) {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			if value, ok := strings.CutPrefix(strings.TrimSpace(param), "q="); ok {
				if q, err := strconv.ParseFloat(value, 64); err == nil && q == 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}
