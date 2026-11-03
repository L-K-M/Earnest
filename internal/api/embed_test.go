package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPFeedLLMConsent(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	// Consent is scoped to credentialled feeds; the default feed is public.
	sub, err := f.svc.Subscribe(t.Context(), f.owner, "https://example.org/private", service.SubscribeOptions{Credentials: &service.FeedCredentials{Username: "u", Password: "p"}, OpID: "private-sub"})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/feeds/%d/llm", sub.FeedID)

	rec := f.do(t, http.MethodGet, path, nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	settings := decodeBody[service.FeedPipelineSettings](t, rec)
	if settings.FeedID != sub.FeedID || settings.Allowed {
		t.Fatal(settings)
	}

	rec = f.do(t, http.MethodPut, path, feedLLMBody{Allowed: true, OpID: "llm-allow"}, f.session())
	requireStatus(t, rec, http.StatusOK)
	if settings = decodeBody[service.FeedPipelineSettings](t, rec); settings.FeedID != sub.FeedID || !settings.Allowed {
		t.Fatal(settings)
	}
	rec = f.do(t, http.MethodGet, path, nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if settings = decodeBody[service.FeedPipelineSettings](t, rec); !settings.Allowed {
		t.Fatal(settings)
	}

	requireStatus(t, f.do(t, http.MethodGet, "/v1/feeds/999999/llm", nil, f.session()), http.StatusNotFound)
}
