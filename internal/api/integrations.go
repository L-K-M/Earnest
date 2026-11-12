package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

type integrationBody struct {
	URL           string  `json:"url" maxLength:"2048"`
	DefaultListID string  `json:"default_list_id,omitempty" maxLength:"256" doc:"Karakeep list ID. Empty leaves bookmarks unfiled."`
	DefaultLabel  string  `json:"default_label,omitempty" maxLength:"256" doc:"Hister label. Hister has labels, not folders."`
	Token         *string `json:"token,omitempty" maxLength:"4096" doc:"Omit to keep the token only if the URL is unchanged; empty clears it. Never returned."`
	OpID          string  `json:"op_id"`
}

type integrationInput struct {
	Provider service.ArticleIntegration `path:"provider" enum:"karakeep,hister"`
	Body     integrationBody
}

type sendArticleInput struct {
	ID       int64                      `path:"id" minimum:"1"`
	Provider service.ArticleIntegration `path:"provider" enum:"karakeep,hister"`
	Body     struct {
		OpID string `json:"op_id"`
	}
}

func (s *server) integrationRoutes() {
	route(s, http.MethodGet, "/v1/integrations", "get-article-integrations", func(ctx context.Context, in *empty) (service.ArticleIntegrations, error) {
		return s.svc.GetArticleIntegrations(ctx, actor(ctx))
	})
	route(s, http.MethodPut, "/v1/integrations/{provider}", "put-article-integration", func(ctx context.Context, in *integrationInput) (service.IntegrationSettings, error) {
		b := in.Body
		return s.svc.PutArticleIntegration(ctx, actor(ctx), in.Provider, service.IntegrationSettingsInput{URL: b.URL, DefaultListID: b.DefaultListID, DefaultLabel: b.DefaultLabel, Token: b.Token, OpID: b.OpID})
	})
	route(s, http.MethodPost, "/v1/entries/{id}/send/{provider}", "send-article", func(ctx context.Context, in *sendArticleInput) (service.SendArticleResult, error) {
		return s.svc.SendArticle(ctx, actor(ctx), in.ID, in.Provider, in.Body.OpID)
	})
}
