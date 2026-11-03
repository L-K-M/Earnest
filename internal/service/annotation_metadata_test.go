package service

import (
	"context"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestAnnotationAuthorAndCreatedAt(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "metadata")
	agent := surfaceAgent(t, f, "Morning Agent", 3)
	request := PutAnnotationRequest{OpID: "metadata", EntryIDs: []int64{id}, Kind: AnnotationBrief, Value: "brief"}
	created, err := f.s.PutAnnotation(ctx, agent, request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetAnnotation(ctx, f.a, agent.PrincipalID, request.EntryIDs, request.Kind)
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.s.ListAnnotations(ctx, f.a, AnnotationQuery{})
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	briefs, err := f.s.TodayBriefs(ctx, f.a)
	if err != nil || len(briefs) != 1 {
		t.Fatal(briefs, err)
	}
	for _, n := range []Annotation{created, got, list[0], briefs[0]} {
		if n.AuthorName != "Morning Agent" || !n.CreatedAt.Equal(f.s.now()) {
			t.Fatal(n)
		}
	}
}
