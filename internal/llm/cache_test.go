package llm

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"testing"
)

// fakeCacheStore is an in-memory CacheStore.
type fakeCacheStore struct {
	entries map[string]fakeCacheEntry
	puts    int
}

type fakeCacheEntry struct {
	owner     *int64
	hash      []byte
	kind      string
	version   string
	model     string
	output    string
	citations []byte
}

func newFakeCacheStore() *fakeCacheStore {
	return &fakeCacheStore{entries: map[string]fakeCacheEntry{}}
}

func (f *fakeCacheStore) LLMCacheGet(ctx context.Context, owner *int64, hash []byte, kind, version, model string) (string, []byte, bool, error) {
	entry, ok := f.entries[cacheTuple(owner, hash, kind, version, model)]
	if !ok {
		return "", nil, false, nil
	}
	return entry.output, entry.citations, true, nil
}

func (f *fakeCacheStore) LLMCachePut(ctx context.Context, owner *int64, hash []byte, kind, version, model, output string, citations []byte) error {
	f.puts++
	f.entries[cacheTuple(owner, hash, kind, version, model)] = fakeCacheEntry{
		owner: owner, hash: hash, kind: kind, version: version, model: model, output: output, citations: citations,
	}
	return nil
}

func cacheTuple(owner *int64, hash []byte, kind, version, model string) string {
	key := kind + "\x00" + version + "\x00" + model
	if owner == nil {
		return "public\x00" + key
	}
	return "user\x00" + strconv.FormatInt(*owner, 10) + "\x00" + key
}

func TestContentKey(t *testing.T) {
	owner := int64(7)
	key := ContentKey(&owner, "article body", CacheBlurb, "v3", "gpt-test")
	if key.Kind != CacheBlurb || key.PromptVersion != "v3" || key.Model != "gpt-test" {
		t.Errorf("key = %+v", key)
	}
	if key.OwnerUserID == nil || *key.OwnerUserID != 7 {
		t.Errorf("owner = %v", key.OwnerUserID)
	}
	want := sha256.Sum256([]byte("article body"))
	if key.ContentHash != want {
		t.Errorf("hash = %x, want %x", key.ContentHash, want)
	}
}

func TestCacheGetMiss(t *testing.T) {
	cache := NewCache(newFakeCacheStore())
	_, err := cache.Get(context.Background(), ContentKey(nil, "body", CacheSummary, "v1", "m"))
	if !errors.Is(err, ErrMiss) {
		t.Fatalf("error = %v, want ErrMiss", err)
	}
}

func TestCachePutGet(t *testing.T) {
	store := newFakeCacheStore()
	cache := NewCache(store)
	key := ContentKey(nil, "body", CacheSummary, "v1", "m")
	citations := []Citation{{EntryID: 3, Start: 4, End: 9, Quote: "passage"}}
	if err := cache.Put(context.Background(), key, "a summary", citations); err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 {
		t.Errorf("puts = %d", store.puts)
	}
	got, err := cache.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "a summary" {
		t.Errorf("output = %q", got.Output)
	}
	if len(got.Citations) != 1 || got.Citations[0].EntryID != 3 || got.Citations[0].Quote != "passage" {
		t.Errorf("citations = %v", got.Citations)
	}
}

func TestCacheOwnerPartitioning(t *testing.T) {
	store := newFakeCacheStore()
	cache := NewCache(store)
	one, two := int64(1), int64(2)
	public := ContentKey(nil, "body", CacheBlurb, "v1", "m")
	ownerOne := ContentKey(&one, "body", CacheBlurb, "v1", "m")
	ownerTwo := ContentKey(&two, "body", CacheBlurb, "v1", "m")
	for key, output := range map[CacheKey]string{public: "public", ownerOne: "one", ownerTwo: "two"} {
		if err := cache.Put(context.Background(), key, output, nil); err != nil {
			t.Fatal(err)
		}
	}
	for key, want := range map[CacheKey]string{public: "public", ownerOne: "one", ownerTwo: "two"} {
		got, err := cache.Get(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Output != want {
			t.Errorf("owner %v output = %q, want %q", key.OwnerUserID, got.Output, want)
		}
	}
}

func TestCachePutAlwaysWritesJSONArray(t *testing.T) {
	store := newFakeCacheStore()
	cache := NewCache(store)
	if err := cache.Put(context.Background(), ContentKey(nil, "body", CacheBlurb, "v1", "m"), "out", nil); err != nil {
		t.Fatal(err)
	}
	for _, entry := range store.entries {
		if string(entry.citations) != "[]" {
			t.Errorf("citations = %s, want []", entry.citations)
		}
	}
}
