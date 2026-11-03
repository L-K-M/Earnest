package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// The store is the persistence boundary the LLM layer programs against.
var (
	_ llm.CacheStore = (*store.Store)(nil)
	_ llm.SpendStore = (*store.Store)(nil)
)

func TestLLMCache(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	owner := createUser(t, db, "cache-owner")
	other := createUser(t, db, "cache-other")
	hash := bytes.Repeat([]byte{0xab}, 32)

	if _, _, found, err := db.LLMCacheGet(ctx, &owner, hash, "blurb", "v1", "m"); err != nil || found {
		t.Fatalf("miss = found %v, err %v", found, err)
	}
	citations := []byte(`[{"entry_id":1,"start":0,"end":4,"quote":"body"}]`)
	if err := db.LLMCachePut(ctx, &owner, hash, "blurb", "v1", "m", "a blurb", citations); err != nil {
		t.Fatal(err)
	}
	output, storedCitations, found, err := db.LLMCacheGet(ctx, &owner, hash, "blurb", "v1", "m")
	if err != nil || !found {
		t.Fatalf("get = found %v, err %v", found, err)
	}
	if output != "a blurb" {
		t.Errorf("output = %q", output)
	}
	var wantCitations, gotCitations []map[string]any
	if err := json.Unmarshal(citations, &wantCitations); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(storedCitations, &gotCitations); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotCitations, wantCitations) {
		t.Errorf("citations = %s, want %s", storedCitations, citations)
	}

	// The same content under another owner, or with no owner, is a
	// different cache entry: private output never leaks across users.
	for _, key := range []*int64{&other, nil} {
		if _, _, found, err := db.LLMCacheGet(ctx, key, hash, "blurb", "v1", "m"); err != nil || found {
			t.Errorf("owner %v: found %v, err %v", key, found, err)
		}
	}

	// A different kind, prompt version or model is a different entry.
	for _, mutation := range []struct{ kind, version, model string }{
		{"summary", "v1", "m"}, {"blurb", "v2", "m"}, {"blurb", "v1", "other"},
	} {
		if _, _, found, err := db.LLMCacheGet(ctx, &owner, hash, mutation.kind, mutation.version, mutation.model); err != nil || found {
			t.Errorf("%+v: found %v, err %v", mutation, found, err)
		}
	}

	// Putting again replaces the output.
	if err := db.LLMCachePut(ctx, &owner, hash, "blurb", "v1", "m", "replaced", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	output, _, found, err = db.LLMCacheGet(ctx, &owner, hash, "blurb", "v1", "m")
	if err != nil || !found || output != "replaced" {
		t.Errorf("replace = %q, found %v, err %v", output, found, err)
	}

	// The public partition replaces the same way, without touching the
	// private row.
	if err := db.LLMCachePut(ctx, nil, hash, "blurb", "v1", "m", "public", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if output, _, found, err = db.LLMCacheGet(ctx, nil, hash, "blurb", "v1", "m"); err != nil || !found || output != "public" {
		t.Errorf("public = %q, found %v, err %v", output, found, err)
	}
	if err := db.LLMCachePut(ctx, nil, hash, "blurb", "v1", "m", "public replaced", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if output, _, found, err = db.LLMCacheGet(ctx, nil, hash, "blurb", "v1", "m"); err != nil || !found || output != "public replaced" {
		t.Errorf("public replace = %q, found %v, err %v", output, found, err)
	}
	if output, _, found, err = db.LLMCacheGet(ctx, &owner, hash, "blurb", "v1", "m"); err != nil || !found || output != "replaced" {
		t.Errorf("private after public replace = %q, found %v, err %v", output, found, err)
	}
}

func TestLLMSpend(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	november := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	spent, limit, err := db.LLMEnsureMonth(ctx, "openai", november, 10000000)
	if err != nil {
		t.Fatal(err)
	}
	if spent != 0 || limit != 10000000 {
		t.Errorf("ensure = spent %d, limit %d", spent, limit)
	}
	if err := db.LLMAddSpend(ctx, "openai", november, 2500000); err != nil {
		t.Fatal(err)
	}
	spent, _, err = db.LLMEnsureMonth(ctx, "openai", november, 10000000)
	if err != nil {
		t.Fatal(err)
	}
	if spent != 2500000 {
		t.Errorf("spent = %d, want 2500000", spent)
	}

	// Configuration is the source of truth for the limit.
	if _, limit, err = db.LLMEnsureMonth(ctx, "openai", november, 5000000); err != nil {
		t.Fatal(err)
	}
	if limit != 5000000 {
		t.Errorf("limit = %d, want 5000000", limit)
	}

	// A new month starts from zero.
	december := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if spent, _, err = db.LLMEnsureMonth(ctx, "openai", december, 5000000); err != nil || spent != 0 {
		t.Errorf("december = spent %d, err %v", spent, err)
	}
}

func TestEmbedding(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	var feed, entry int64
	if err := db.Pool().QueryRow(ctx, "INSERT INTO feeds(url) VALUES('https://example.com/rss') RETURNING id").Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO entries(feed_id,seq,identity_hash) VALUES($1,1,decode(repeat('ab',32),'hex')) RETURNING id", feed).Scan(&entry); err != nil {
		t.Fatal(err)
	}

	if _, _, found, err := db.EmbeddingGet(ctx, entry, "model"); err != nil || found {
		t.Fatalf("miss = found %v, err %v", found, err)
	}

	// A short vector is zero-padded to the storage width, and dims keeps the
	// model's own width.
	vector := make([]float32, llm.StorageDimensions)
	vector[0] = 0.5
	vector[1] = -0.25
	if err := db.EmbeddingPut(ctx, entry, "model", 2, vector); err != nil {
		t.Fatal(err)
	}
	dims, stored, found, err := db.EmbeddingGet(ctx, entry, "model")
	if err != nil || !found {
		t.Fatalf("get = found %v, err %v", found, err)
	}
	if dims != 2 || len(stored) != llm.StorageDimensions {
		t.Fatalf("dims = %d, components = %d", dims, len(stored))
	}
	if stored[0] != 0.5 || stored[1] != -0.25 {
		t.Errorf("head = %v", stored[:2])
	}
	for i, value := range stored[2:] {
		if value != 0 {
			t.Fatalf("component %d = %v, want zero padding", i+2, value)
		}
	}

	// A full-width vector round-trips exactly.
	full := make([]float32, llm.StorageDimensions)
	for i := range full {
		full[i] = float32(i+1) / 7
	}
	if err := db.EmbeddingPut(ctx, entry, "model", llm.StorageDimensions, full); err != nil {
		t.Fatal(err)
	}
	dims, stored, _, err = db.EmbeddingGet(ctx, entry, "model")
	if err != nil {
		t.Fatal(err)
	}
	if dims != llm.StorageDimensions || len(stored) != llm.StorageDimensions {
		t.Fatalf("dims = %d, components = %d", dims, len(stored))
	}
	// halfvec stores 16-bit components, so a full-width vector round-trips
	// to within float16 precision rather than exactly.
	for i := range full {
		if math.Abs(float64(stored[i])-float64(full[i])) > 1e-3*math.Max(1, math.Abs(float64(full[i]))) {
			t.Fatalf("component %d = %v, want approximately %v", i, stored[i], full[i])
		}
	}

	// Dimensions outside 1..len(vector) are rejected.
	if err := db.EmbeddingPut(ctx, entry, "model", llm.StorageDimensions+1, full); err == nil {
		t.Error("expected an error for out-of-range dims")
	}
	// A non-finite component is rejected rather than stored.
	bad := append([]float32(nil), full...)
	bad[0] = float32(math.NaN())
	if err := db.EmbeddingPut(ctx, entry, "model", 1, bad); err == nil {
		t.Error("expected an error for a NaN component")
	}

	// Exponent notation and values at the float16 limits round-trip.
	extreme := make([]float32, llm.StorageDimensions)
	extreme[0] = 1e-7
	extreme[1] = 65504
	if err := db.EmbeddingPut(ctx, entry, "extreme", 2, extreme); err != nil {
		t.Fatal(err)
	}
	if _, stored, _, err := db.EmbeddingGet(ctx, entry, "extreme"); err != nil {
		t.Fatal(err)
	} else if stored[0] <= 0 || stored[0] > 2e-7 || stored[1] != 65504 {
		t.Errorf("extreme = %v, %v", stored[0], stored[1])
	}
}
