package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/danielgtaylor/huma/v2"
)

type mediaInput struct {
	ID          int64  `path:"id" minimum:"1"`
	Version     string `query:"v" doc:"Version from the listed URL. A matching version is cached for good; any other value still returns the current image."`
	IfNoneMatch string `header:"If-None-Match"`
}

type mediaOutput struct {
	Status       int
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	ETag         string `header:"ETag"`
	Body         []byte
}

// immutableMedia lets a browser keep a versioned image without asking again:
// a new image always gets a new version and so a new URL.
const (
	immutableMedia   = "private, max-age=31536000, immutable"
	revalidatedMedia = "private, no-cache"
)

// mediaRoutes serve icons and thumbnails stored at ingest. They never fetch
// from a publisher, so loading them reveals nothing outside Earnest.
func (s *server) mediaRoutes() {
	register := func(path, id, summary string, load func(context.Context, service.Actor, int64) (service.MediaFile, error)) {
		huma.Register(s.api, huma.Operation{
			Method: http.MethodGet, Path: path, OperationID: id, Summary: summary,
			Security: []map[string][]string{{"bearer": {}}, {"session": {}}},
			Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable},
			Responses: map[string]*huma.Response{
				"200": {Description: "The stored image", Content: map[string]*huma.MediaType{
					"image/png":  {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					"image/jpeg": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				}},
				"304": {Description: "The cached copy is current"},
			},
		}, func(ctx context.Context, in *mediaInput) (*mediaOutput, error) {
			file, err := load(ctx, actor(ctx), in.ID)
			if err != nil {
				return nil, s.problem(err)
			}
			cache := revalidatedMedia
			if in.Version == file.ETag {
				cache = immutableMedia
			}
			etag := `"` + file.ETag + `"`
			if etagMatches(in.IfNoneMatch, etag) {
				return &mediaOutput{Status: http.StatusNotModified, CacheControl: cache, ETag: etag}, nil
			}
			return &mediaOutput{Status: http.StatusOK, ContentType: file.ContentType, CacheControl: cache, ETag: etag, Body: file.Body}, nil
		})
	}
	register("/v1/entries/{id}/thumbnail", "entry-thumbnail", "Get an article's stored thumbnail", s.svc.EntryThumbnail)
	register("/v1/subscriptions/{id}/icon", "subscription-icon", "Get a subscription's stored feed icon", s.svc.SubscriptionIcon)
}

// etagMatches implements the weak comparison If-None-Match uses.
func etagMatches(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
