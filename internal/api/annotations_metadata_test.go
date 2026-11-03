package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPAnnotationMetadata(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	agent, principal := surfaceAgent(t, f)
	rec := f.do(t, http.MethodPost, "/v1/briefs", briefBody{EntryIDs: []int64{f.entryID}, Value: "hello", OpID: "metadata"}, agent)
	requireStatus(t, rec, http.StatusOK)
	created := decodeBody[service.Annotation](t, rec)
	rec = f.do(t, http.MethodGet, "/v1/briefs/today", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	briefs := decodeBody[[]service.Annotation](t, rec)
	if len(briefs) != 1 || briefs[0].PrincipalID != principal || briefs[0].AuthorName != "Morning Agent" || briefs[0].CreatedAt.IsZero() || !briefs[0].CreatedAt.Equal(created.CreatedAt) {
		t.Fatal(briefs, created)
	}
	rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/annotations?principal_id=%d", principal), nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	list := decodeBody[[]service.Annotation](t, rec)
	if len(list) != 1 || list[0].AuthorName != "Morning Agent" || list[0].CreatedAt.IsZero() {
		t.Fatal(list)
	}
}
