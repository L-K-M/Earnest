package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// CacheKind is the schema's llm_cache kind. It separates outputs by purpose
// so a blurb is never served for a summary.
type CacheKind string

const (
	CacheLabel   CacheKind = "label"
	CacheScore   CacheKind = "score"
	CacheBrief   CacheKind = "brief"
	CacheSummary CacheKind = "summary"
	CacheBlurb   CacheKind = "blurb"
	CacheEdition CacheKind = "edition"
)

// CacheKey identifies one cached model output.
type CacheKey struct {
	// OwnerUserID partitions private content per user. Nil means public
	// content, which is shared across users.
	OwnerUserID *int64
	// ContentHash is the SHA-256 of the content the output was generated
	// from.
	ContentHash [32]byte
	// Kind is the pipeline purpose of the output.
	Kind CacheKind
	// PromptVersion separates outputs produced by different prompts.
	PromptVersion string
	// Model is the model that produced the output.
	Model string
}

// ContentKey builds a key from the content an output describes.
func ContentKey(ownerUserID *int64, content string, kind CacheKind, promptVersion, model string) CacheKey {
	return CacheKey{
		OwnerUserID:   ownerUserID,
		ContentHash:   sha256.Sum256([]byte(content)),
		Kind:          kind,
		PromptVersion: promptVersion,
		Model:         model,
	}
}

// Citation is one passage a model output points at. The pipeline verifies
// citations against the cited content version before display.
type Citation struct {
	EntryID int64  `json:"entry_id"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Quote   string `json:"quote"`
}

// CachedOutput is one llm_cache row.
type CachedOutput struct {
	Output    string
	Citations []Citation
	CreatedAt time.Time
}

// CacheStore reads and writes llm_cache. *store.Store implements it.
type CacheStore interface {
	// LLMCacheGet returns the cached output and citations JSON. found is
	// false when no row matches.
	LLMCacheGet(ctx context.Context, ownerUserID *int64, contentHash []byte, kind, promptVersion, model string) (output string, citations []byte, found bool, err error)
	// LLMCachePut inserts or replaces the cached output.
	LLMCachePut(ctx context.Context, ownerUserID *int64, contentHash []byte, kind, promptVersion, model, output string, citations []byte) error
}

// Cache reads and writes cached outputs.
type Cache struct {
	store CacheStore
}

// NewCache returns a cache backed by store.
func NewCache(store CacheStore) *Cache {
	return &Cache{store: store}
}

// Get returns the cached output for key. An absent entry is ErrMiss, which a
// caller treats as a miss rather than a failure.
func (c *Cache) Get(ctx context.Context, key CacheKey) (*CachedOutput, error) {
	output, citations, found, err := c.store.LLMCacheGet(ctx, key.OwnerUserID, key.ContentHash[:], string(key.Kind), key.PromptVersion, key.Model)
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("%w: %s/%s", ErrMiss, key.Kind, key.Model)
	}
	var parsed []Citation
	if len(citations) > 0 {
		if err := json.Unmarshal(citations, &parsed); err != nil {
			return nil, fmt.Errorf("decode cached citations: %w", err)
		}
	}
	return &CachedOutput{Output: output, Citations: parsed}, nil
}

// Put stores output under key, replacing any earlier output.
func (c *Cache) Put(ctx context.Context, key CacheKey, output string, citations []Citation) error {
	if citations == nil {
		citations = []Citation{}
	}
	body, err := json.Marshal(citations)
	if err != nil {
		return fmt.Errorf("encode citations: %w", err)
	}
	if err := c.store.LLMCachePut(ctx, key.OwnerUserID, key.ContentHash[:], string(key.Kind), key.PromptVersion, key.Model, output, body); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}
