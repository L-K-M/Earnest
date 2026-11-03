package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

// feedLLMBody flips a feed's consent to send publisher text to a model
// provider. op_id deduplicates retries like other mutations.
type feedLLMBody struct {
	Allowed bool   `json:"allowed"`
	OpID    string `json:"op_id"`
}

func (s *server) embedRoutes() {
	route(s, http.MethodGet, "/v1/pipeline", "pipeline-status", func(ctx context.Context, _ *empty) (service.PipelineStatus, error) {
		return s.svc.PipelineStatus(ctx, actor(ctx))
	})
	route(s, http.MethodGet, "/v1/feeds/{id}/llm", "feed-llm-consent", func(ctx context.Context, in *idInput) (service.FeedPipelineSettings, error) {
		return s.svc.FeedLLMConsent(ctx, actor(ctx), in.ID)
	})
	route(s, http.MethodPut, "/v1/feeds/{id}/llm", "set-feed-llm-consent", func(ctx context.Context, in *idBody[feedLLMBody]) (service.FeedPipelineSettings, error) {
		return s.svc.SetFeedLLMConsent(ctx, actor(ctx), in.ID, in.Body.Allowed, in.Body.OpID)
	})
}
