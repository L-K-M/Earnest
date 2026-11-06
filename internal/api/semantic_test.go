package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPPipelineStatusAndSemantic(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("no engine reports disabled", func(t *testing.T) {
		f := newAPIFixture(t, template)
		rec := f.do(t, http.MethodGet, "/v1/pipeline", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		status := decodeBody[service.PipelineStatus](t, rec)
		for task, value := range map[string]string{"embed": status.Embed, "classify": status.Classify, "semantic": status.Semantic, "watch": status.Watch, "blurb": status.Blurb, "summary": status.Summary, "edition": status.Edition, "ask": status.Ask, "compile_rule": status.CompileRule} {
			if value != statusDisabled {
				t.Fatalf("%s status=%q", task, value)
			}
		}
	})
	t.Run("semantic paths answer in-band, never 500", func(t *testing.T) {
		f := newAPIFixture(t, template)
		for _, path := range []string{"/v1/search?q=stored&mode=hybrid", fmt.Sprintf("/v1/entries/%d/similar", f.entryID)} {
			rec := f.do(t, http.MethodGet, path, nil, f.session())
			requireStatus(t, rec, http.StatusOK)
			page := decodeBody[searchPage](t, rec)
			if page.Status != statusDisabled || page.Matches == nil || len(page.Matches) != 0 {
				t.Fatalf("%s: %+v", path, page)
			}
		}
		// Full-text search does not involve the pipeline and carries no status.
		rec := f.do(t, http.MethodGet, "/v1/search?q=stored", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[searchPage](t, rec)
		if page.Status != "" || len(page.Matches) != 1 {
			t.Fatal(page)
		}
		rec = f.do(t, http.MethodGet, "/v1/search?q=stored&mode=bogus", nil, f.session())
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid mode status %d", rec.Code)
		}
	})
	t.Run("cluster groups annotate every entry", func(t *testing.T) {
		f := newAPIFixture(t, template)
		rec := f.do(t, http.MethodGet, "/v1/entries?view=all&group=cluster", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[entriesPage](t, rec)
		if len(page.Entries) != 1 || len(page.Groups) != 1 || page.Groups[0].Entry.ID != f.entryID || page.Groups[0].ClusterID != 0 || page.Groups[0].AlsoIn != 0 {
			t.Fatal(page)
		}
	})
}

func TestHTTPEntriesIncludeCachedBlurb(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	var principal int64
	if err := f.db.Pool().QueryRow(t.Context(), `INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Summaries') RETURNING id`, f.owner.UserID).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(t.Context(), `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev,content_hash) SELECT $1,$2,'blurb','Stored plain text',ARRAY[id],1,content_hash FROM entries WHERE id=$3`, f.owner.UserID, principal, f.entryID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/entries?view=all", "/v1/entries?view=all&group=cluster", fmt.Sprintf("/v1/entries?ids=%d", f.entryID)} {
		rec := f.do(t, http.MethodGet, path, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[entriesPage](t, rec)
		if len(page.Entries) != 1 || page.Entries[0].Blurb == nil || *page.Entries[0].Blurb != "Stored plain text" {
			t.Fatalf("%s: %+v", path, page)
		}
	}
}

func TestHTTPPipelineBudgetAvailability(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		vector := make([]float32, llm.StorageDimensions)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	egress, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	const capMicros = 10000
	cfg := llm.Config{
		Providers: map[string]llm.ProviderConfig{"fake": {Kind: llm.KindOpenAI, BaseURL: upstream.URL, MonthlyLimitUSD: capMicros}},
		Models:    map[string]llm.ModelInfo{"test": {Dimensions: llm.StorageDimensions, EmbeddingPer1M: 1000000}},
		Tasks:     map[llm.Task]llm.TaskConfig{},
	}
	for _, task := range []llm.Task{llm.TaskEmbed, llm.TaskClassify, llm.TaskBlurb, llm.TaskSummary, llm.TaskEdition, llm.TaskAsk, llm.TaskCompileRule} {
		cfg.Tasks[task] = llm.TaskConfig{Provider: "fake", Model: "test"}
	}
	router, err := llm.NewRouter(cfg, egress)
	if err != nil {
		t.Fatal(err)
	}
	budget := llm.NewBudget(f.db, cfg.Limits())
	client, err := llm.NewClient(router, budget, cfg.Models)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(router, client, llm.NewCache(f.db), cfg.Models)
	if err != nil {
		t.Fatal(err)
	}
	handler := f.handler
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(service.WithPipeline(r.Context(), engine)))
	})
	checkStatus := func(want string) {
		t.Helper()
		before := calls.Load()
		rec := f.do(t, http.MethodGet, "/v1/pipeline", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		var statuses map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
			t.Fatal(err)
		}
		for task, status := range statuses {
			if status != want {
				t.Errorf("%s = %s, want %s", task, status, want)
			}
		}
		if calls.Load() != before {
			t.Fatal("status called provider")
		}
	}
	checkPage := func(path, want string) {
		t.Helper()
		rec := f.do(t, http.MethodGet, path, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[searchPage](t, rec)
		if page.Status != want {
			t.Fatalf("%s status = %s, want %s", path, page.Status, want)
		}
	}
	similar := fmt.Sprintf("/v1/entries/%d/similar", f.entryID)
	checkStatus(statusEnabled)
	checkPage(similar, statusPaused) // No source embedding yet.
	checkPage("/v1/search?q=stored&mode=hybrid", statusEnabled)
	// A hybrid saved view reports the semantic path in-band.
	saved := savedSearchBody{Name: "Hybrid stored", Query: "stored", Filters: service.SearchFilter{Mode: service.SearchHybrid, Limit: 1}}
	rec := f.do(t, http.MethodPost, "/v1/saved-searches", saved, f.session())
	requireStatus(t, rec, http.StatusOK)
	created := decodeBody[savedSearch](t, rec)
	rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/saved-searches/%d/entries", created.ID), nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if page := decodeBody[searchPage](t, rec); page.Status != statusEnabled {
		t.Fatalf("hybrid saved search status = %q", page.Status)
	}
	// Warm a source embedding using the same routed, reserved path as backfill.
	vector, _, err := client.Embed(t.Context(), llm.TaskEmbed, llm.EmbedRequest{Texts: []string{"Stored content"}})
	if err != nil {
		t.Fatal(err)
	}
	literal, err := json.Marshal(vector[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(t.Context(), `INSERT INTO embeddings(entry_id,model,content_hash,input_title,embedding,dims,input_hash)
        SELECT id,'test',content_hash,title,$2::halfvec,1024,content_hash FROM entries WHERE id=$1`, f.entryID, string(literal)); err != nil {
		t.Fatal(err)
	}
	checkPage(similar, statusEnabled)
	spend, err := budget.Spend(t.Context(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	hold, err := budget.Reserve(t.Context(), "fake", capMicros-spend.Spent)
	if err != nil {
		t.Fatal(err)
	}
	checkStatus(statusPaused)
	before := calls.Load()
	checkPage("/v1/search?q=fresh&mode=hybrid", statusPaused)
	// Cached retrieval needs no provider budget.
	checkPage("/v1/search?q=stored&mode=hybrid", statusEnabled)
	checkPage(similar, statusEnabled)
	if calls.Load() != before {
		t.Fatal("paused or cached retrieval called provider")
	}
	if err := budget.Reconcile(t.Context(), "fake", hold, 0); err != nil {
		t.Fatal(err)
	}
	checkStatus(statusEnabled)
}

func TestSimilarPageOmitsNextCursor(t *testing.T) {
	rows := []service.SearchResult{{EntryID: 1, Seq: 2, Rank: 0.5}}
	page, err := similarPage(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "" {
		t.Fatalf("next cursor = %q, want empty", page.NextCursor)
	}
	if len(page.Matches) != 1 || page.Matches[0].EntryID != 1 {
		t.Fatalf("matches = %+v", page.Matches)
	}
}
