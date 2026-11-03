package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

const (
	statusEnabled  = "enabled"
	statusDisabled = "disabled"
	statusPaused   = "paused"
)

// pipelineErr reports an unavailable pipeline task as 503 rather than the
// generic 500; the service's disabled or paused reason stays in the detail.
func pipelineErr(err error) error {
	if errors.Is(err, service.ErrPipelineDisabled) || errors.Is(err, service.ErrPipelinePaused) {
		p := problemStatus(http.StatusServiceUnavailable)
		p.Detail = err.Error()
		return p
	}
	return err
}

// semanticPage reports the semantic path in-band: a successful call marks the
// page enabled, while toSearchPage marks a disabled or paused one.
func semanticPage(rows []service.SearchResult, err error) (searchPage, error) {
	page, err := toSearchPage(rows, err)
	if err == nil && page.Status == "" {
		page.Status = statusEnabled
	}
	return page, err
}

type similarInput struct {
	ID    int64 `path:"id" minimum:"1"`
	Limit int   `query:"limit" default:"10" minimum:"1" maximum:"100"`
}

func (s *server) semanticRoutes() {
	route(s, http.MethodGet, "/v1/entries/{id}/similar", "similar-entries", func(ctx context.Context, in *similarInput) (searchPage, error) {
		rows, err := s.svc.Similar(ctx, actor(ctx), in.ID, in.Limit)
		return semanticPage(rows, err)
	})
}
