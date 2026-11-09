package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestMediaRoutes(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	ctx := t.Context()
	var body bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&body, img); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_images(entry_id,candidates,status,content_type,body,etag) VALUES($1,ARRAY['https://example.org/a.png'],'ready','image/png',$2,'0123456789abcdef')
 ON CONFLICT(entry_id) DO UPDATE SET candidates=EXCLUDED.candidates,status=EXCLUDED.status,content_type=EXCLUDED.content_type,body=EXCLUDED.body,etag=EXCLUDED.etag`, f.entryID, body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO feed_icons(feed_id,status,content_type,body,etag) SELECT feed_id,'ready','image/png',$2,'fedcba9876543210' FROM subscriptions WHERE id=$1
 ON CONFLICT(feed_id) DO UPDATE SET status=EXCLUDED.status,content_type=EXCLUDED.content_type,body=EXCLUDED.body,etag=EXCLUDED.etag`, f.subID, body.Bytes()); err != nil {
		t.Fatal(err)
	}
	cookieOnly := requestCredentials{cookie: f.cookie}
	thumbnail := "/v1/entries/" + strconv.FormatInt(f.entryID, 10) + "/thumbnail"
	icon := "/v1/subscriptions/" + strconv.FormatInt(f.subID, 10) + "/icon"

	t.Run("lists carry versioned paths", func(t *testing.T) {
		rec := f.do(t, http.MethodGet, "/v1/entries?view=all", nil, cookieOnly)
		requireStatus(t, rec, http.StatusOK)
		var page struct {
			Entries []struct {
				ThumbnailURL string `json:"thumbnail_url"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Entries) != 1 {
			t.Fatalf("page=%s: %v", rec.Body.String(), err)
		}
		if page.Entries[0].ThumbnailURL != thumbnail+"?v=0123456789abcdef" {
			t.Fatalf("thumbnail_url=%q", page.Entries[0].ThumbnailURL)
		}
		rec = f.do(t, http.MethodGet, "/v1/subscriptions", nil, cookieOnly)
		requireStatus(t, rec, http.StatusOK)
		var subs []struct {
			IconURL string `json:"icon_url"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &subs); err != nil || len(subs) != 1 {
			t.Fatalf("subscriptions=%s: %v", rec.Body.String(), err)
		}
		if subs[0].IconURL != icon+"?v=fedcba9876543210" {
			t.Fatalf("icon_url=%q", subs[0].IconURL)
		}
	})

	t.Run("serves stored bytes with cache headers", func(t *testing.T) {
		for _, tc := range []struct{ path, etag string }{{thumbnail, "0123456789abcdef"}, {icon, "fedcba9876543210"}} {
			rec := f.do(t, http.MethodGet, tc.path+"?v="+tc.etag, nil, cookieOnly)
			requireStatus(t, rec, http.StatusOK)
			if !bytes.Equal(rec.Body.Bytes(), body.Bytes()) {
				t.Fatalf("%s served %d bytes", tc.path, rec.Body.Len())
			}
			h := rec.Header()
			if h.Get("Content-Type") != "image/png" || h.Get("ETag") != `"`+tc.etag+`"` || h.Get("Cache-Control") != immutableMedia || h.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("%s headers=%v", tc.path, h)
			}
			// A stale or missing version must not be cached for good.
			rec = f.do(t, http.MethodGet, tc.path+"?v=stale", nil, cookieOnly)
			requireStatus(t, rec, http.StatusOK)
			if rec.Header().Get("Cache-Control") != revalidatedMedia {
				t.Fatalf("%s stale cache=%q", tc.path, rec.Header().Get("Cache-Control"))
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tc.path, nil)
			req.AddCookie(f.cookie)
			req.Header.Set("If-None-Match", `W/"other", "`+tc.etag+`"`)
			conditional := newRecorder(f, req)
			requireStatus(t, conditional, http.StatusNotModified)
			if conditional.Body.Len() != 0 {
				t.Fatalf("%s 304 body=%q", tc.path, conditional.Body.String())
			}
			if conditional.Header().Get("ETag") != `"`+tc.etag+`"` || conditional.Header().Get("Cache-Control") != revalidatedMedia {
				t.Fatalf("%s 304 headers=%v", tc.path, conditional.Header())
			}
			req.Header.Set("If-None-Match", `"other"`)
			miss := newRecorder(f, req)
			requireStatus(t, miss, http.StatusOK)
			if !bytes.Equal(miss.Body.Bytes(), body.Bytes()) {
				t.Fatalf("%s revalidation served %d bytes", tc.path, miss.Body.Len())
			}
		}
	})

	t.Run("requires authentication and visibility", func(t *testing.T) {
		requireStatus(t, f.do(t, http.MethodGet, thumbnail, nil, requestCredentials{}), http.StatusUnauthorized)
		requireStatus(t, f.do(t, http.MethodGet, icon, nil, requestCredentials{}), http.StatusUnauthorized)
		requireStatus(t, f.do(t, http.MethodGet, "/v1/entries/999999/thumbnail", nil, cookieOnly), http.StatusNotFound)
		requireStatus(t, f.do(t, http.MethodGet, "/v1/subscriptions/999999/icon", nil, cookieOnly), http.StatusNotFound)
		other, err := f.auth.CreateUser(ctx, "other", "password")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.EntryThumbnail(ctx, other, f.entryID); err == nil {
			t.Fatal("another user read the thumbnail")
		}
		if _, err := f.svc.SubscriptionIcon(ctx, other, f.subID); err == nil {
			t.Fatal("another user read the icon")
		}
	})
}

func newRecorder(f *apiFixture, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}
