package api

import (
	"context"
	"github.com/L-K-M/Earnest/internal/llm"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

func (s *server) llmSettingsRoutes() {
	route(s, http.MethodPost, "/v1/settings/llm/models/check", "check-llm-model", func(ctx context.Context, in *bodyInput[service.LLMModelCheckInput]) (llm.ModelCheck, error) {
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return s.svc.CheckLLMModel(ctx, actor(ctx), in.Body)
	})
	route(s, http.MethodPost, "/v1/settings/llm/models", "discover-llm-models", func(ctx context.Context, in *bodyInput[service.LLMModelsInput]) (service.LLMModels, error) {
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return s.svc.DiscoverLLMModels(ctx, actor(ctx), in.Body)
	})
	route(s, http.MethodGet, "/v1/settings/llm", "get-llm-settings", func(ctx context.Context, _ *empty) (service.LLMSettings, error) {
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return s.svc.GetLLMSettings(ctx, actor(ctx))
	})
	route(s, http.MethodPut, "/v1/settings/llm", "put-llm-settings", func(ctx context.Context, in *bodyInput[service.LLMSettingsInput]) (service.LLMSettings, error) {
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return s.svc.PutLLMSettings(ctx, actor(ctx), in.Body)
	})
}
