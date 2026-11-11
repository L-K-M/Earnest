package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/jackc/pgx/v5"
)

type modelRuntime struct {
	revision int64
	engine   *pipeline.Engine
	batch    pipeline.EditionBatch
}

// ModelContext pins one immutable configuration to an HTTP request or worker
// pass. The database is checked at each boundary, including on other server
// processes. In-flight work finishes with its original routing and budget;
// subsequent work sees committed settings without restarting the server.
func (s *Service) ModelContext(ctx context.Context) (context.Context, error) {
	// Most operations need only the revision, not another copy of every key
	// and model price. Decrypt and rebuild only when configuration changes.
	var revision int64
	err := s.store.Pool().QueryRow(ctx, "SELECT revision FROM llm_settings WHERE singleton").Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ctx, fmt.Errorf("read LLM settings revision: %w", err)
	}
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.modelRuntime == nil || s.modelRuntime.revision < revision {
		cfg, current, err := s.loadLLMSettings(ctx, s.store.Pool())
		if err != nil {
			return ctx, err
		}
		runtime, err := s.buildModelRuntime(cfg, current)
		if err != nil {
			return ctx, fmt.Errorf("build LLM runtime: %w", ErrUnavailable)
		}
		s.modelRuntime = runtime
	}
	return s.withModelRuntime(ctx, s.modelRuntime), nil
}

func (s *Service) buildModelRuntime(cfg llm.Config, revision int64) (*modelRuntime, error) {
	client := s.outbound
	if client == nil {
		var err error
		client, err = outbound.New(outbound.Config{})
		if err != nil {
			return nil, err
		}
	}
	router, err := llm.NewRouter(cfg, client)
	if err != nil {
		return nil, err
	}
	budget := llm.NewBudget(s.store, cfg.Limits())
	modelClient, err := llm.NewClient(router, budget, cfg.Models)
	if err != nil {
		return nil, err
	}
	engine, err := pipeline.New(router, modelClient, llm.NewCache(s.store), cfg.Models)
	if err != nil {
		return nil, err
	}
	batch := pipeline.NewEditionBatch(router, budget, cfg, client)
	if batch != nil {
		raw, err := json.Marshal(editionBatchConfig(cfg))
		if err != nil {
			return nil, err
		}
		// Bind persisted batch handles to their original provider, credentials,
		// model and prices. A later configuration must not poll or cancel that
		// handle at a different destination or reconcile it using different prices.
		identity := s.modelConfigIdentity(raw)
		batch = &configuredEditionBatch{
			EditionBatch: batch, prefix: editionConfigPrefix + identity + ":", allowLegacy: revision == 0,
			retain: func(ctx context.Context) error {
				return s.retainEditionConfig(ctx, s.store.Pool(), identity, raw, editionConfigBound)
			},
		}
	}
	return &modelRuntime{revision: revision, engine: engine, batch: batch}, nil
}

type configuredEditionBatch struct {
	pipeline.EditionBatch
	prefix      string
	allowLegacy bool
	retain      func(context.Context) error
}

func (b *configuredEditionBatch) Submit(ctx context.Context, requests []llm.CompleteRequest) (string, error) {
	if b.retain != nil {
		if err := b.retain(ctx); err != nil {
			return "", err
		}
	}
	id, err := b.EditionBatch.Submit(ctx, requests)
	if err != nil {
		return "", err
	}
	return b.prefix + id, nil
}

func (b *configuredEditionBatch) originalHandle(id string) (string, error) {
	if handle, ok := strings.CutPrefix(id, b.prefix); ok {
		return handle, nil
	}
	if b.allowLegacy && !strings.HasPrefix(id, editionConfigPrefix) {
		return id, nil
	}
	return "", llm.ErrUnsupported
}

func (b *configuredEditionBatch) Poll(ctx context.Context, id string, requests []llm.CompleteRequest) ([]string, bool, error) {
	handle, err := b.originalHandle(id)
	if err != nil {
		return nil, false, err
	}
	return b.EditionBatch.Poll(ctx, handle, requests)
}

func (b *configuredEditionBatch) Cancel(ctx context.Context, id string) error {
	handle, err := b.originalHandle(id)
	if err != nil {
		return err
	}
	return b.EditionBatch.Cancel(ctx, handle)
}
