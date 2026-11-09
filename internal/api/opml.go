package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/danielgtaylor/huma/v2"
)

const maxOPMLBytes = 8 << 20

type opmlInput struct{ RawBody []byte }
type opmlOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (s *server) opmlRoutes() {
	routeUpload(s, http.MethodPost, "/v1/opml:import", "import-opml", maxOPMLBytes, func(ctx context.Context, in *opmlInput) ([]service.OPMLResult, error) {
		body, err := uploadBody(ctx, in.RawBody, maxOPMLBytes, "file", "opml")
		if err != nil {
			return nil, err
		}
		doc, err := feed.ParseOPML(body)
		if err != nil {
			return nil, fmt.Errorf("parse OPML: %w", service.ErrInvalid)
		}
		return s.svc.ImportOPML(ctx, actor(ctx), toServiceOutlines(doc.Outlines))
	})
	s.api.OpenAPI().Paths["/v1/opml:import"].Post.RequestBody.Content = map[string]*huma.MediaType{
		"application/xml":     {Schema: &huma.Schema{Type: "string"}},
		"text/x-opml":         {Schema: &huma.Schema{Type: "string"}},
		"multipart/form-data": {Schema: &huma.Schema{Type: "object", Properties: map[string]*huma.Schema{"file": {Type: "string", Format: "binary"}}, Required: []string{"file"}}},
	}
	huma.Register(s.api, huma.Operation{Method: http.MethodGet, Path: "/v1/opml", OperationID: "export-opml", Security: []map[string][]string{{"bearer": {}}, {"session": {}}}, Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable}, Responses: map[string]*huma.Response{"200": {Description: "OPML 2.0 subscription document", Content: map[string]*huma.MediaType{"text/x-opml": {Schema: &huma.Schema{Type: "string"}}}}}}, func(ctx context.Context, in *empty) (*opmlOutput, error) {
		outlines, err := s.svc.ExportOPML(ctx, actor(ctx))
		if err != nil {
			return nil, s.problem(err)
		}
		body, err := feed.GenerateOPML(&feed.OPML{Title: "Earnest subscriptions", Outlines: toFeedOutlines(outlines)})
		if err != nil {
			return nil, s.problem(err)
		}
		return &opmlOutput{ContentType: "text/x-opml; charset=utf-8", Body: body}, nil
	})
}

// uploadBody returns an uploaded file: the raw request body, or the first
// multipart/form-data part that carries a file name or one of the given form
// field names. Either way it holds at most limit bytes.
func uploadBody(ctx context.Context, body []byte, limit int64, fields ...string) ([]byte, error) {
	contentType := request(ctx).Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, service.ErrInvalid
	}
	if media == "multipart/form-data" {
		reader := multipart.NewReader(io.LimitReader(bytes.NewReader(body), limit+1), params["boundary"])
		found := false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, service.ErrInvalid
			}
			if part.FileName() == "" && !slices.Contains(fields, part.FormName()) {
				_ = part.Close()
				continue
			}
			body, err = io.ReadAll(io.LimitReader(part, limit+1))
			_ = part.Close()
			if err != nil {
				return nil, fmt.Errorf("read upload: %w", service.ErrInvalid)
			}
			found = true
			break
		}
		if !found {
			return nil, service.ErrInvalid
		}
	}
	if int64(len(body)) > limit {
		return nil, service.ErrInvalid
	}
	return body, nil
}

func toServiceOutlines(outlines []feed.Outline) []service.OPMLOutline {
	result := make([]service.OPMLOutline, 0, len(outlines))
	for _, o := range outlines {
		result = append(result, service.OPMLOutline{Title: o.Title, XMLURL: o.XMLURL, HTMLURL: o.HTMLURL, Outlines: toServiceOutlines(o.Children)})
	}
	return result
}
func toFeedOutlines(outlines []service.OPMLOutline) []feed.Outline {
	result := make([]feed.Outline, 0, len(outlines))
	for _, o := range outlines {
		result = append(result, feed.Outline{Title: o.Title, XMLURL: o.XMLURL, HTMLURL: o.HTMLURL, Children: toFeedOutlines(o.Outlines)})
	}
	return result
}
