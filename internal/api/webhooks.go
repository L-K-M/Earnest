package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

type webhookBody struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	OpID   string   `json:"op_id"`
}
type webhookPatch struct {
	URL    *string  `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`
	Active *bool    `json:"active,omitempty"`
	OpID   string   `json:"op_id"`
}

func (s *server) webhookRoutes() {
	route(s, http.MethodGet, "/v1/webhooks", "list-webhooks", func(ctx context.Context, in *empty) ([]service.Webhook, error) {
		return s.svc.Webhooks(ctx, actor(ctx))
	})
	route(s, http.MethodPost, "/v1/webhooks", "create-webhook", func(ctx context.Context, in *bodyInput[webhookBody]) (service.WebhookSecret, error) {
		return s.svc.CreateWebhook(ctx, actor(ctx), service.WebhookInput{URL: in.Body.URL, Events: in.Body.Events}, in.Body.OpID)
	})
	route(s, http.MethodPatch, "/v1/webhooks/{id}", "patch-webhook", func(ctx context.Context, in *idBody[webhookPatch]) (service.Webhook, error) {
		return s.svc.UpdateWebhook(ctx, actor(ctx), in.ID, service.WebhookPatch{URL: in.Body.URL, Events: in.Body.Events, Active: in.Body.Active}, in.Body.OpID)
	})
	route(s, http.MethodDelete, "/v1/webhooks/{id}", "delete-webhook", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.DeleteWebhook(ctx, actor(ctx), in.ID, in.OpID)
	})
}
