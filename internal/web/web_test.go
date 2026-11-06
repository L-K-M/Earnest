package web

import (
	"net/http"
	"net/http/httptest"
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
