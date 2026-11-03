package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

// A watch is a notify-only semantic rule: the embedded description matches
// continuously, optionally confirmed by an LLM check within a daily budget.
type watchBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	CheckLLM    bool   `json:"check_llm"`
	Budget      int    `json:"budget"`
	CheckBudget int    `json:"check_budget"`
}

func (s *server) watchRoutes() {
	route(s, http.MethodGet, "/v1/watch-rules", "list-watch-rules", func(ctx context.Context, _ *empty) ([]service.WatchRule, error) {
		watches, err := s.svc.Watches(ctx, actor(ctx))
		return watches, pipelineErr(err)
	})
	route(s, http.MethodPost, "/v1/watch-rules", "create-watch-rule", func(ctx context.Context, in *bodyInput[watchBody]) (service.WatchRule, error) {
		b := in.Body
		watch, err := s.svc.CreateWatch(ctx, actor(ctx), service.WatchRule{
			Rule: service.Rule{
				Name:      b.Name,
				Predicate: service.RulePredicate{Type: service.RuleSemantic, Value: b.Description},
				Actions:   []service.RuleAction{{Type: service.RuleNotify, Budget: b.Budget}},
				Enabled:   b.Enabled,
			},
			CheckLLM:    b.CheckLLM,
			CheckBudget: b.CheckBudget,
		})
		return watch, pipelineErr(err)
	})
	route(s, http.MethodDelete, "/v1/watch-rules/{id}", "delete-watch-rule", func(ctx context.Context, in *idInput) (empty, error) {
		return empty{}, pipelineErr(s.svc.DeleteWatch(ctx, actor(ctx), in.ID))
	})
}
