package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDirectoryPathFallsBackToIndex(t *testing.T) {
	root := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("index")},
		"assets/app.js": &fstest.MapFile{Data: []byte("app")},
	}
	handler := newHandler(root, "https://content.example")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("directory status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "index" {
		t.Fatalf("directory body = %q, want the SPA index", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "app" {
		t.Fatalf("asset = %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/later", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "index" {
		t.Fatalf("client route = %d %q", rec.Code, rec.Body.String())
	}
}

func TestAssetsPreferPrecompressedCopies(t *testing.T) {
	root := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("index")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("plain")},
		"assets/app.js.br": &fstest.MapFile{Data: []byte("brotli")},
		"assets/app.js.gz": &fstest.MapFile{Data: []byte("gzip")},
		"assets/only.css":  &fstest.MapFile{Data: []byte("css")},
	}
	handler := newHandler(root, "https://content.example")
	cases := []struct{ accept, path, body, encoding string }{
		{"gzip, deflate, br", "/assets/app.js", "brotli", "br"},
		{"gzip", "/assets/app.js", "gzip", "gzip"},
		{"br;q=0, gzip;q=0.8", "/assets/app.js", "gzip", "gzip"},
		{"", "/assets/app.js", "plain", ""},
		{"br", "/assets/only.css", "css", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.accept != "" {
			req.Header.Set("Accept-Encoding", c.accept)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != c.body {
			t.Fatalf("%q %s = %d %q, want %q", c.accept, c.path, rec.Code, rec.Body.String(), c.body)
		}
		if got := rec.Header().Get("Content-Encoding"); got != c.encoding {
			t.Fatalf("%q %s encoding = %q, want %q", c.accept, c.path, got, c.encoding)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("%s must vary by Accept-Encoding", c.path)
		}
		if c.path == "/assets/app.js" && !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
			t.Fatalf("encoded asset content type = %q", rec.Header().Get("Content-Type"))
		}
	}
}
