package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestKeptImportAPI(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	export := []byte(`{"title": "Starred", "items": [
		{"title": "Kept here", "guid": "https://example.org/kept", "canonical": [{"href": "https://example.org/kept"}],
		 "categories": ["user/-/state/com.google/read", "user/-/state/com.google/starred"],
		 "origin": {"streamId": "feed/16", "feedUrl": "https://example.org/feed", "title": "Example"},
		 "crawlTimeMsec": "1700000000000", "published": 1699999000, "content": {"content": "<p>Body</p>"}},
		{"title": "Elsewhere", "guid": "tag:elsewhere.example,2026:1",
		 "origin": {"feedUrl": "https://elsewhere.example/feed", "title": "Elsewhere", "htmlUrl": "https://elsewhere.example/"},
		 "crawlTimeMsec": "1700000500000"}
	]}`)
	upload := func(body []byte, contentType, query string, credentials requestCredentials) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/kept:import"+query, bytes.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		if credentials.cookie != nil {
			request.AddCookie(credentials.cookie)
			request.Header.Set(csrfHeader, credentials.csrf)
		}
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, request)
		return rec
	}
	report := func(rec *httptest.ResponseRecorder) service.KeptImportReport {
		t.Helper()
		requireStatus(t, rec, http.StatusOK)
		var out service.KeptImportReport
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	statuses := func(r service.KeptImportReport) string {
		parts := []string{}
		for _, result := range r.Results {
			parts = append(parts, string(result.Status))
		}
		return strings.Join(parts, ",")
	}

	first := report(upload(export, "application/json", "", f.session()))
	if got := statuses(first); got != "created,not_subscribed" {
		t.Fatalf("raw JSON import = %s: %+v", got, first.Results)
	}
	if first.Results[1].FeedURL != "https://elsewhere.example/feed" || first.Results[1].Title != "Elsewhere" || len(first.Subscribed) != 0 {
		t.Fatalf("skipped result = %+v, subscribed = %+v", first.Results[1], first.Subscribed)
	}

	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	if err := form.WriteField("description", "upload"); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("file", "starred.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(export); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	second := report(upload(buf.Bytes(), form.FormDataContentType(), "?missing_feeds=subscribe", f.session()))
	if got := statuses(second); got != "unchanged,created" {
		t.Fatalf("multipart subscribe import = %s: %+v", got, second.Results)
	}
	if len(second.Subscribed) != 1 || second.Subscribed[0].URL != "https://elsewhere.example/feed" {
		t.Fatalf("subscribed = %+v", second.Subscribed)
	}

	rec := f.do(t, http.MethodGet, "/v1/entries?view=kept", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	for _, result := range append(first.Results[:1], second.Results[1]) {
		if !strings.Contains(rec.Body.String(), fmt.Sprintf(`"id":%d`, result.EntryID)) {
			t.Fatalf("kept view lacks entry %d: %s", result.EntryID, rec.Body.String())
		}
	}

	for _, tc := range []struct {
		name, body, query string
		status            int
		detail            string
	}{
		{"not an export", `<opml/>`, "", http.StatusBadRequest, "not a Google Reader JSON export"},
		{"broken JSON", `{"items": [`, "", http.StatusBadRequest, "not valid JSON"},
		{"unknown policy", `{"items": []}`, "?missing_feeds=always", http.StatusUnprocessableEntity, "validation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := upload([]byte(tc.body), "application/json", tc.query, f.session())
			requireStatus(t, rec, tc.status)
			if !strings.Contains(rec.Body.String(), tc.detail) {
				t.Fatalf("problem = %s, want it to mention %q", rec.Body.String(), tc.detail)
			}
		})
	}
	requireStatus(t, upload(export, "application/json", "", requestCredentials{}), http.StatusUnauthorized)
}

func TestUploadBody(t *testing.T) {
	upload := func(body []byte, contentType string, limit int64) ([]byte, error) {
		r := httptest.NewRequest(http.MethodPost, "/v1/kept:import", bytes.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		return uploadBody(context.WithValue(context.Background(), requestKey{}, r), body, limit, "file")
	}
	multipartBody := func(field, filename, content string) ([]byte, string) {
		var buf bytes.Buffer
		form := multipart.NewWriter(&buf)
		var err error
		if filename == "" {
			err = form.WriteField(field, content)
		} else {
			var part interface{ Write([]byte) (int, error) }
			part, err = form.CreateFormFile(field, filename)
			if err == nil {
				_, err = part.Write([]byte(content))
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes(), form.FormDataContentType()
	}
	if got, err := upload([]byte("raw"), "application/json", 8); err != nil || string(got) != "raw" {
		t.Fatalf("raw = %q, %v", got, err)
	}
	if _, err := upload([]byte("too long"), "application/json", 4); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("raw over the limit: %v", err)
	}
	body, contentType := multipartBody("file", "starred.json", "{}")
	if got, err := upload(body, contentType, 1<<10); err != nil || string(got) != "{}" {
		t.Fatalf("multipart file = %q, %v", got, err)
	}
	body, contentType = multipartBody("description", "", "no file here")
	if _, err := upload(body, contentType, 1<<10); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("multipart without a file: %v", err)
	}
	body, contentType = multipartBody("file", "starred.json", strings.Repeat("x", 100))
	if _, err := upload(body, contentType, 50); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("multipart file over the limit: %v", err)
	}
	if _, err := upload([]byte("x"), "multipart/form-data", 8); !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("multipart without a boundary: %v", err)
	}
}
