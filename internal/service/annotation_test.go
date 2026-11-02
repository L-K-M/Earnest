package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestAgentAnnotations(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	agent := surfaceAgent(t, f, "Morning", 10)
	other := surfaceAgent(t, f, "Evening", 10)
	for _, kind := range []AnnotationKind{AnnotationSummary, AnnotationBlurb} {
		_, err := f.s.PutAnnotation(ctx, agent, PutAnnotationRequest{OpID: string(kind), EntryIDs: []int64{id}, Kind: kind, Value: "secret"})
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s accepted: %v", kind, err)
		}
	}
	r := PutAnnotationRequest{OpID: "note", EntryIDs: []int64{id}, Kind: AnnotationNote, Value: "hello\x00\x1bworld"}
	n, err := f.s.PutAnnotation(ctx, agent, r)
	if err != nil || n.Value != "helloworld" {
		t.Fatal(n, err)
	}
	r.Value = "retry does not change text"
	again, err := f.s.PutAnnotation(ctx, agent, r)
	if err != nil || again.ID != n.ID || again.Value != n.Value {
		t.Fatal("op replay", again, err)
	}
	for _, a := range []Actor{agent, other, f.a} {
		got, err := f.s.GetAnnotation(ctx, a, agent.PrincipalID, []int64{id}, AnnotationNote)
		if err != nil || got.Untrusted != (a.PrincipalID == other.PrincipalID) {
			t.Fatal("untrusted", got, err)
		}
	}
	for i, ids := range [][]int64{{id}, {id, f.ingest(t, "another")}} {
		_, err := f.s.PutAnnotation(ctx, agent, PutAnnotationRequest{OpID: strings.Repeat("brief", i+1), EntryIDs: ids, Kind: AnnotationBrief, Value: "morning brief"})
		if err != nil {
			t.Fatal(err)
		}
	}
	briefs, err := f.s.TodayBriefs(ctx, other)
	if err != nil || len(briefs) != 1 || len(briefs[0].EntryIDs) != 2 || !briefs[0].Untrusted {
		t.Fatal(briefs, err)
	}
	_, err = f.s.PutAnnotation(ctx, agent, PutAnnotationRequest{OpID: "label", EntryIDs: []int64{id}, Kind: AnnotationLabel, Value: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	var labels int
	if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM labels").Scan(&labels); err != nil || labels != 0 {
		t.Fatal("agent label entered native rules", labels, err)
	}
	if err := f.s.DeleteAnnotation(ctx, agent, []int64{id}, AnnotationNote, "delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetAnnotation(ctx, f.a, agent.PrincipalID, []int64{id}, AnnotationNote); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	var updated, deleted int
	if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FILTER(WHERE type='annotation.updated'),count(*) FILTER(WHERE type='annotation.deleted') FROM changes").Scan(&updated, &deleted); err != nil || updated != 4 || deleted != 1 {
		t.Fatal("change events", updated, deleted, err)
	}
	if _, err := f.s.PutAnnotation(ctx, agent, PutAnnotationRequest{OpID: "foreign", EntryIDs: []int64{999999}, Kind: AnnotationNote, Value: "no"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestPlainAgentText(t *testing.T) {
	for _, tc := range []struct {
		in      string
		limit   int
		want    string
		invalid bool
	}{
		{"hello\x00\nworld", 20, "helloworld", false},
		{"éé", 2, "éé", false},
		{"ééé", 2, "", true},
		{"\x00\n", 20, "", true},
		{string([]byte{0xff}), 20, "", true},
		{"<b>literal</b> **text**", 30, "<b>literal</b> **text**", false},
	} {
		got, err := plainText(tc.in, tc.limit)
		if errors.Is(err, ErrInvalid) != tc.invalid || (!tc.invalid && got != tc.want) {
			t.Fatalf("%q: got %q %v", tc.in, got, err)
		}
	}
}

func TestAnnotationPagesSearchAndPipeline(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	agent := surfaceAgent(t, f, "Morning", 3)
	other := surfaceAgent(t, f, "Evening", 3)
	n, err := f.s.PutAnnotation(ctx, agent, PutAnnotationRequest{OpID: "note", EntryIDs: []int64{id}, Kind: AnnotationNote, Value: "needle note"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.s.ListAnnotations(ctx, other, AnnotationQuery{EntryIDs: []int64{id}, Kind: AnnotationNote, Limit: 1})
	if err != nil || len(page) != 1 || !page[0].Untrusted || page[0].ID != n.ID {
		t.Fatal("annotation page", page, err)
	}
	hits, err := f.s.SearchAnnotations(ctx, other, "needle", 1)
	if err != nil || len(hits) != 1 || hits[0].AuthorKind != PrincipalAgent || hits[0].AuthorName != "Morning" || !hits[0].Untrusted {
		t.Fatal("search attribution", hits, err)
	}
	if err := f.s.DeleteAnnotation(ctx, agent, []int64{id}, AnnotationNote, "delete"); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ListAnnotations(ctx, other, AnnotationQuery{IncludeDeleted: true})
	if err != nil || len(page) != 1 || !page[0].Deleted || page[0].Rev <= n.Rev {
		t.Fatal("annotation tombstone", page, err)
	}
	var payload []byte
	if err := f.db.Pool().QueryRow(ctx, "SELECT payload FROM changes WHERE user_id=$1 AND type='annotation.deleted'", f.a.UserID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Object     syncmodel.Object
		Annotation Annotation
	}
	if err := json.Unmarshal(payload, &event); err != nil || !event.Object.Deleted || event.Object.ID != syncmodel.ID(n.ID) || !event.Annotation.Untrusted {
		t.Fatal("sync tombstone", event, err)
	}
	pipeline := Actor{UserID: f.a.UserID, Kind: PrincipalPipeline, Via: CredentialInternal, Scopes: []Scope{ScopeAnnotationsWrite, ScopeEntriesRead}}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Summarizer') RETURNING id", f.a.UserID).Scan(&pipeline.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PutAnnotation(ctx, pipeline, PutAnnotationRequest{OpID: "summary", EntryIDs: []int64{id}, Kind: AnnotationSummary, Value: "internal summary"}); err != nil {
		t.Fatal("pipeline summary denied", err)
	}
	if _, err := f.s.PutAnnotation(ctx, f.a, PutAnnotationRequest{OpID: "human-summary", EntryIDs: []int64{id}, Kind: AnnotationSummary, Value: "not pipeline"}); !errors.Is(err, ErrForbidden) {
		t.Fatal("human wrote pipeline summary", err)
	}
}
