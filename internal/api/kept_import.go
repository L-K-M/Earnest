package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/danielgtaylor/huma/v2"
)

const (
	// maxKeptImportBytes fits roughly ten thousand articles with their bodies.
	maxKeptImportBytes = 64 << 20
	maxKeptImportItems = 10000
	// keptImportParseTimeout bounds the walk of a large, authenticated upload.
	keptImportParseTimeout = time.Minute
)

type keptImportInput struct {
	MissingFeeds service.MissingFeeds `query:"missing_feeds" enum:"skip,subscribe" default:"skip" doc:"What to do with articles whose feed you don't follow: skip them, or subscribe to the feed first."`
	RawBody      []byte
}

func (s *server) keptImportRoutes() {
	const path = "/v1/kept:import"
	routeUpload(s, http.MethodPost, path, "import-kept", maxKeptImportBytes, func(ctx context.Context, in *keptImportInput) (service.KeptImportReport, error) {
		body, err := uploadBody(ctx, in.RawBody, maxKeptImportBytes, "file")
		if err != nil {
			return service.KeptImportReport{}, err
		}
		export, err := feed.ParseReaderExport(body, feed.Limits{MaxBytes: maxKeptImportBytes, MaxItems: maxKeptImportItems, Timeout: keptImportParseTimeout})
		if err != nil {
			return service.KeptImportReport{}, fmt.Errorf("%s: %w", readerExportProblem(err), service.ErrInvalid)
		}
		return s.svc.ImportKept(ctx, actor(ctx), toKeptImportItems(export.Items), in.MissingFeeds)
	})
	op := s.api.OpenAPI().Paths[path].Post
	op.Summary = "Import starred articles into Kept"
	op.Description = "Keeps the articles of a Google Reader JSON export, such as the starred-articles export of FreshRSS, Inoreader or Google Takeout, up to 64 MiB and 10,000 articles. Articles Earnest already has are kept; others are stored from the export. The export's labels are added to each article. The import never rewrites existing articles, never brings back an article you un-kept, a label you deleted or a label you removed from an article, and keeps imported articles out of the river."
	op.RequestBody.Content = map[string]*huma.MediaType{
		"application/json":    {Schema: &huma.Schema{Type: "object", Description: "Google Reader stream contents: an object with an items array."}},
		"multipart/form-data": {Schema: &huma.Schema{Type: "object", Properties: map[string]*huma.Schema{"file": {Type: "string", Format: "binary"}}, Required: []string{"file"}}},
	}
}

// readerExportProblem explains a parse failure in terms of the uploaded file.
func readerExportProblem(err error) string {
	switch {
	case errors.Is(err, feed.ErrTooLarge):
		return fmt.Sprintf("the export is larger than %d MiB", maxKeptImportBytes>>20)
	case errors.Is(err, feed.ErrTooManyItems):
		return fmt.Sprintf("the export holds more than %d articles", maxKeptImportItems)
	case errors.Is(err, feed.ErrParseTimeout):
		return "the export took too long to read"
	case errors.Is(err, feed.ErrUnsupportedFormat):
		return "the file is not a Google Reader JSON export"
	default:
		return "the export is not valid JSON"
	}
}

func toKeptImportItems(items []feed.ReaderItem) []service.KeptImportItem {
	out := make([]service.KeptImportItem, len(items))
	for i, it := range items {
		enclosures := make([]service.Enclosure, len(it.Item.Enclosures))
		for j, e := range it.Item.Enclosures {
			enclosures[j] = service.Enclosure{URL: e.URL, Type: e.Type, Length: e.Length}
		}
		out[i] = service.KeptImportItem{
			FeedURL: it.FeedURL, FeedTitle: it.FeedTitle, SiteURL: it.SiteURL,
			GUID: it.Item.GUID, URL: it.Item.URL, Title: it.Item.Title, Author: it.Item.Author,
			PublishedAt: it.Item.Published, UpdatedAt: it.Item.Updated,
			ContentHTML: it.Item.ContentHTML, SummaryHTML: it.Item.SummaryHTML,
			Enclosures: enclosures, Read: it.Read, CrawledAt: it.Crawled, Labels: it.Labels,
		}
	}
	return out
}
