package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const llmBatchConfigPurpose = "earnest/llm-batch-config/v1"
const editionConfigPrefix = "llm-settings:"

type modelContextKey struct{}
type managedModelContext struct {
	service *Service
	runtime *modelRuntime
}

// Keep only the routes an edition can use, not credentials for unrelated tasks.
func editionBatchConfig(cfg llm.Config) llm.Config {
	out := llm.Config{Providers: map[string]llm.ProviderConfig{}, Tasks: map[llm.Task]llm.TaskConfig{}, Models: map[string]llm.ModelInfo{}}
	for _, task := range []llm.Task{llm.TaskEdition, llm.TaskBlurb, llm.TaskSummary} {
		if route, ok := cfg.Tasks[task]; ok {
			out.Tasks[task] = route
			out.Providers[route.Provider] = cfg.Providers[route.Provider]
			info, _ := llm.LookupModel(cfg.Models, route.Provider, route.Model)
			out.Models[llm.ModelKey(route.Provider, route.Model)] = info
		}
	}
	return out
}

// Key the identity as well as encrypting the snapshot: a public digest of
// configuration containing a weak local-server key would permit offline guesses.
func (s *Service) modelConfigIdentity(raw []byte) string {
	mac := hmac.New(sha256.New, s.modelIdentityKey)
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) withModelRuntime(ctx context.Context, runtime *modelRuntime) context.Context {
	ctx = WithPipeline(ctx, runtime.engine)
	ctx = pipeline.WithEditionBatch(ctx, runtime.batch)
	return context.WithValue(ctx, modelContextKey{}, managedModelContext{service: s, runtime: runtime})
}

// An asynchronous batch outlives its worker pass and may outlive the process.
// Resolve its original routing and prices before regenerating requests or
// polling it; a settings edit must not abandon paid work or send its handle
// to a different provider. Explicit test/evaluation contexts stay untouched.
func (s *Service) editionModelContext(ctx context.Context, handle string) (context.Context, error) {
	managed, _ := ctx.Value(modelContextKey{}).(managedModelContext)
	if managed.service != s || handle == "" {
		return ctx, nil
	}
	var envelope []byte
	var hash string
	legacy := !strings.HasPrefix(handle, editionConfigPrefix)
	if legacy {
		if managed.runtime.revision == 0 {
			return ctx, nil
		}
		if err := s.store.Pool().QueryRow(ctx, "SELECT config_hash,config_encrypted FROM llm_batch_configs WHERE legacy").Scan(&hash, &envelope); err != nil {
			return ctx, fmt.Errorf("original legacy batch configuration unavailable: %w", ErrUnavailable)
		}
	} else {
		identity, _, ok := strings.Cut(strings.TrimPrefix(handle, editionConfigPrefix), ":")
		hash = identity
		if !ok || len(hash) != sha256.Size*2 {
			return ctx, fmt.Errorf("invalid edition batch configuration identity: %w", ErrUnavailable)
		}
		if err := s.store.Pool().QueryRow(ctx, "SELECT config_encrypted FROM llm_batch_configs WHERE config_hash=$1", hash).Scan(&envelope); err != nil {
			return ctx, fmt.Errorf("original edition batch configuration unavailable: %w", ErrUnavailable)
		}
	}
	raw, err := s.openSecret(envelope, llmBatchConfigPurpose+":"+hash)
	if err != nil {
		return ctx, fmt.Errorf("decrypt edition batch configuration: %w", ErrUnavailable)
	}
	var cfg llm.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ctx, fmt.Errorf("decode edition batch configuration: %w", ErrUnavailable)
	}
	revision := int64(1)
	if legacy {
		revision = 0
	}
	runtime, err := s.buildModelRuntime(cfg, revision)
	if err != nil || runtime.batch == nil {
		return ctx, fmt.Errorf("restore edition batch configuration: %w", ErrUnavailable)
	}
	return s.withModelRuntime(ctx, runtime), nil
}

type editionConfigOrigin uint8

const (
	editionConfigBound editionConfigOrigin = iota
	editionConfigLegacy
)

type editionConfigWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Retain before submission, so a returned handle is always recoverable even
// if Settings changes concurrently or the next poll runs on another process.
func (s *Service) retainEditionConfig(ctx context.Context, q editionConfigWriter, hash string, raw []byte, origin editionConfigOrigin) error {
	envelope, err := s.sealSecret(raw, llmBatchConfigPurpose+":"+hash)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO llm_batch_configs(config_hash,config_encrypted,legacy) VALUES($1,$2,$3)
  ON CONFLICT(config_hash) DO UPDATE SET legacy=llm_batch_configs.legacy OR EXCLUDED.legacy`, hash, envelope, origin == editionConfigLegacy)
	if err != nil {
		return fmt.Errorf("retain edition batch configuration: %w", err)
	}
	return nil
}

// Before the first Settings save, legacy handles belong to the environment
// runtime. Preserve that one source while switching to database configuration.
func (s *Service) retainLegacyEditionConfig(ctx context.Context, tx pgx.Tx, cfg llm.Config) error {
	route, enabled := cfg.Tasks[llm.TaskEdition]
	if !enabled || cfg.Providers[route.Provider].Kind != llm.KindOpenAI {
		return nil
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind='edition'
  AND coalesce(payload->>'batch_id','')<>'' AND payload->>'batch_id' NOT LIKE 'llm-settings:%')`).Scan(&pending); err != nil {
		return err
	}
	if !pending {
		return nil
	}
	raw, err := json.Marshal(editionBatchConfig(cfg))
	if err != nil {
		return fmt.Errorf("encode legacy batch configuration: %w", err)
	}
	return s.retainEditionConfig(ctx, tx, s.modelConfigIdentity(raw), raw, editionConfigLegacy)
}

// The edition worker owns its advisory lock while updating job references.
// Prune in the same transaction, so a completed job releases its historical
// credentials atomically and cannot race another worker's batch submission.
func (s *Service) pruneEditionConfigs(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DELETE FROM llm_batch_configs c WHERE NOT EXISTS(
  SELECT 1 FROM jobs j WHERE j.kind='edition' AND coalesce(j.payload->>'batch_id','')<>'' AND (
   (split_part(j.payload->>'batch_id',':',1)='llm-settings' AND split_part(j.payload->>'batch_id',':',2)=c.config_hash)
   OR (c.legacy AND j.payload->>'batch_id' NOT LIKE 'llm-settings:%')))`)
	if err != nil {
		return fmt.Errorf("prune edition batch configurations: %w", err)
	}
	return nil
}
