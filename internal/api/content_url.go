package api

import (
	"context"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/web"
)

type contentURLInput struct {
	ID   int64         `path:"id" minimum:"1"`
	Mode web.ImageMode `query:"mode" enum:"proxied,click" doc:"Image loading mode; agent tokens always get click-to-load."`
}

type contentURLResult struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// contentURLRoutes issues the same signed content URLs the web content
// handler accepts, so native apps can open articles without a session.
func (s *server) contentURLRoutes() {
	route(s, http.MethodGet, "/v1/entries/{id}/content-url", "entry-content-url", func(ctx context.Context, in *contentURLInput) (contentURLResult, error) {
		if s.signer == nil {
			return contentURLResult{}, service.ErrUnavailable
		}
		a := actor(ctx)
		mode := web.ImageProxied
		if a.Kind == service.PrincipalAgent {
			// An agent must not influence whether remote images load.
			mode = web.ImageClickToLoad
		} else if in.Mode == web.ImageClickToLoad {
			mode = web.ImageClickToLoad
		}
		// Authorisation belongs to the service; the body is not needed here.
		if _, err := s.svc.Content(ctx, a, in.ID); err != nil {
			return contentURLResult{}, err
		}
		url, expiresAt := s.signer.Issue(a, in.ID, mode)
		return contentURLResult{URL: url, ExpiresAt: expiresAt}, nil
	})
}
