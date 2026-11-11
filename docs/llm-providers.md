# LLM providers

Earnest can call models for embeddings, classification, blurbs, summaries,
editions, rule compilation and ask-my-feeds. This page describes the provider
layer in `internal/llm`, the configuration that drives it and the safety
rules it enforces. The product rationale is in
[research/llm.md](research/llm.md).

## Off by default

Every task is disabled unless both its provider and model are configured.
With no saved configuration or `EARNEST_LLM_*` variables, every task reports
`llm.ErrDisabled` and no model request leaves the process. Settings always
shows the LLM services editor, including when every task is disabled.

## Provider kinds

| Kind | Dialect | Base URL default |
| --- | --- | --- |
| `openai` | OpenAI chat completions and embeddings. The same dialect covers OpenAI, Ollama, llama.cpp and similar servers. | `https://api.openai.com/v1` |
| `anthropic` | Anthropic Messages. No embeddings API, so `Embed` returns `llm.ErrUnsupported`. | `https://api.anthropic.com` |
| `gemini` | Google `generateContent` and `embedContent`. | `https://generativelanguage.googleapis.com/v1beta` |

The base URL is configuration, not code: point it at a compatible server and
the kind selects the wire dialect. For OpenAI-compatible servers the base URL
includes the version segment (`/v1` for OpenAI and Ollama, `/v1beta` for
Gemini).

## Configure in Settings

Open **Settings > LLM services** in the web, Mac or Android client. These
settings apply to the whole server, not just your account or device:

1. Add a named provider, choose OpenAI-compatible, Anthropic or Gemini, and
   enter its API base URL. Supply a key if the service needs one. Set a
   monthly USD cap, timeout and, for older compatible servers, `max_tokens`.
2. Review the model price table. Add your model ID and its input, output and
   embedding prices per million tokens. Local models can have explicit zero
   prices. Embedding models also need the correct dimensions and truncation
   setting. Unknown models are never silently treated as free.
3. Choose a provider and model for each task you want to enable. Leave a
   task's provider and model empty to disable it.
4. Save. New HTTP requests and worker passes use the saved configuration
   without restarting. Work already in progress finishes with its original
   configuration. Saving validates configuration but does not call a provider
   or verify its credentials.

Keys are write-only. A blank key field preserves the existing key; use the
explicit clear control to remove it. Changing a keyed provider's URL or
protocol requires a replacement key or explicit removal, so an old secret
cannot silently travel to a new destination. The server encrypts the saved
configuration using `EARNEST_SECRET_KEY`; keep that key with database backups.
Clients never store provider keys in their offline cache or preferences.

The editor loads deployment environment defaults until the first save
(revision zero). Afterwards the saved database configuration replaces those
defaults, including after a restart. Clearing all task routes and saving keeps
models disabled even if the deployment still has LLM environment variables.
Environment changes no longer override a saved configuration.

Reads and writes use `GET` and `PUT /v1/settings/llm`. They require a human
principal with the `admin` scope, which normal sessions and native app tokens
hold. Agent and Google Reader credentials cannot manage this configuration.
Writes include a revision and an `op_id`; a stale revision returns 409 rather
than overwriting another client's changes. Reload and review before saving
again. Only redacted responses enter the operation log; provider keys never
enter the change feed.

The outbound private-host allowlist remains deployment configuration. A local
model server such as `ollama:11434` still needs
`EARNEST_OUTBOUND_ALLOW_PRIVATE=ollama:11434` and a service restart. Settings
cannot bypass address checks. Prices and caps are exact decimal USD with up
to six fractional digits; zero cap means unlimited. The editor supports up to
32 providers and 128 models, and timeouts from 1 to 600 seconds.

Edition batch handles are bound to the configuration that submitted them. If
that configuration changes while a batch is pending, Earnest will not send
the old handle to a different provider. The edition falls back to synchronous
generation when due, with a cancellation warning; the old provider may still
charge for its batch and its conservative reservation is retained.

## Environment defaults

All variables are optional. Provider names are case-insensitive and stored
lowercase; use letters, digits and dashes. The binary reads them from its own
environment, so a deployment must pass them through (`.env`, compose
`environment`, or the platform's secret store).

| Variable | Meaning |
| --- | --- |
| `EARNEST_LLM_PROVIDER_<NAME>_KIND` | `openai`, `anthropic` or `gemini`. Required for a provider. |
| `EARNEST_LLM_PROVIDER_<NAME>_BASE_URL` | API root. Required for a provider. |
| `EARNEST_LLM_PROVIDER_<NAME>_API_KEY` | API key. Omit for a local server that needs none. Never logged. |
| `EARNEST_LLM_PROVIDER_<NAME>_LIMIT_USD` | Hard monthly spend cap for this provider, decimal USD. Zero or unset means no cap. |
| `EARNEST_LLM_PROVIDER_<NAME>_TIMEOUT` | Per-request timeout, a Go duration. Default `60s`. |
| `EARNEST_LLM_PROVIDER_<NAME>_TOKEN_FIELD` | `max_completion_tokens` (default) or `max_tokens`. Use `max_tokens` for older OpenAI-compatible servers. |
| `EARNEST_LLM_TASK_<TASK>_PROVIDER` | Provider name for the task. |
| `EARNEST_LLM_TASK_<TASK>_MODEL` | Model ID for the task. The model must have a price entry (see below). |
| `EARNEST_LLM_MODELS` | JSON object overlaying the price table. |

The task segment is the upper-case task name: `EMBED`, `CLASSIFY`, `BLURB`,
`SUMMARY`, `EDITION`, `COMPILE_RULE`, `ASK`. A task is enabled only when both
its `_PROVIDER` and `_MODEL` are set, so a half-configured task is a startup
error rather than a silent no-op.

### Example

```sh
EARNEST_LLM_PROVIDER_OPENAI_KIND=openai
EARNEST_LLM_PROVIDER_OPENAI_BASE_URL=https://api.openai.com/v1
EARNEST_LLM_PROVIDER_OPENAI_API_KEY=sk-...
EARNEST_LLM_PROVIDER_OPENAI_LIMIT_USD=20

EARNEST_LLM_PROVIDER_OLLAMA_KIND=openai
EARNEST_LLM_PROVIDER_OLLAMA_BASE_URL=http://ollama:11434/v1
EARNEST_LLM_PROVIDER_OLLAMA_TOKEN_FIELD=max_tokens

EARNEST_LLM_TASK_EMBED_PROVIDER=openai
EARNEST_LLM_TASK_EMBED_MODEL=text-embedding-3-small
EARNEST_LLM_TASK_BLURB_PROVIDER=ollama
EARNEST_LLM_TASK_BLURB_MODEL=llama3.1
```

The BLURB task needs a chat model, not an embedding model. Reasoning
models (for example qwen3 or deepseek-r1 on Ollama) can spend the whole
`max_tokens` budget on hidden reasoning and return empty content, which
Earnest rejects; use a non-reasoning model for these tasks. Add a local
model with a zero price in `EARNEST_LLM_MODELS` when it is not in the
built-in table.

A local server is a private address. `internal/outbound` denies private
addresses unless `EARNEST_OUTBOUND_ALLOW_PRIVATE` names them, so add
`ollama:11434`. The provider layer also derives the non-standard port from the
base URL and passes it to outbound, which otherwise refuses ports other than
80 and 443.

## Per-task routing

`llm.Load` reads the variables into a `Config`. `llm.NewRouter` builds one
provider per configured name and validates that every enabled task's model
has a price entry. `Router.Task` resolves a task to a `Route` (provider and
model) or returns `llm.ErrDisabled`. The model always comes from
configuration: `Client.Complete` and `Client.Embed` overwrite any model the
caller set.

## Budgets and prices

Spend is exact decimal money, stored in `llm_spend` as `numeric(14,6)` and
handled in Go as integer micro-dollars (`llm.USD`). There is no
floating-point budget arithmetic.

`EARNEST_LLM_MODELS` overlays the built-in price table. An entry starts from
the default when one exists, so a partial entry overrides only the fields it
names:

```json
{
  "my-model": {"input_per_1m": 1.5, "output_per_1m": 3, "dimensions": 512, "truncatable": true},
  "text-embedding-3-small": {"embedding_per_1m": 0.01}
}
```

| Field | Meaning |
| --- | --- |
| `input_per_1m` | USD per million input (prompt) tokens. |
| `output_per_1m` | USD per million output (completion) tokens. |
| `embedding_per_1m` | USD per million embedding tokens. |
| `dimensions` | The model's native embedding width. At most 1024 unless `truncatable` is set. |
| `truncatable` | Whether vectors longer than 1024 may be cut to fit. |

The built-in table carries the prices from the research cost section
(`text-embedding-3-small`, `claude-haiku-5-5`, `claude-sonnet-5-5`) and the
geometry of the local embedding models it names (`nomic-embed-text`,
`bge-m3`). A task whose model is not in the table is a startup error, so a
typo cannot call an unpriced model. Add local models with a zero price.

`Client.Complete` and `Client.Embed` atomically reserve a conservative cost
estimate before calling a provider. The monthly check includes other in-flight
reservations; a call that would exceed the configured limit returns a
`*llm.BudgetError`, which matches `llm.ErrBudgetExhausted`. The pipeline pauses
rather than failing the operation. Completion estimates include input bytes,
message framing, the schema and the output maximum, using configured prices.

After the call, the reservation is reconciled with reported billable usage.
Missing usage or an uncertain outcome retains the estimate: a lost response,
including a mid-request connection failure, may still have been billed. A
DNS, TLS, policy or non-timeout dial failure refunds the reservation, as do
explicit unbilled provider rejections. Concurrent calls do not bypass the cap
through a separate check-and-record race.

## Cache

`llm.Cache` reads and writes `llm_cache`. A key is the content hash, the task
kind, the prompt version and the model:

```go
key := llm.ContentKey(&userID, content, llm.CacheBlurb, "v3", model)
cached, err := cache.Get(ctx, key)   // llm.ErrMiss when absent
err = cache.Put(ctx, key, output, citations)
```

`owner_user_id` partitions private content per user. A nil owner is public
content and is shared; a non-nil owner is visible only to that user. The
schema's `NULLS NOT DISTINCT` unique constraint makes the two disjoint. Agent
text never populates this cache or the embeddings table.

The kind is one of the schema's `llm_cache.kind` values: `label`, `score`,
`brief`, `summary`, `blurb`, `edition`. The pipeline maps its task to a kind;
`compile_rule` and `ask` are not cached. A cached output is only as current as
its prompt version, so a prompt change must bump the version.

## Embeddings

`Client.Embed` requests `llm.StorageDimensions` (1024) components unless the
caller names another width, calls the provider, and normalises every vector
to exactly 1024:

- A vector shorter than 1024 is zero-padded. The schema's `CHECK` constraint
  requires the padding to be exactly zero, and `dims` records the model's own
  width.
- A vector longer than 1024 is truncated only when the model's
  `truncatable` flag is set. Cutting a model that is not Matryoshka-style
  destroys its meaning, so such a model is rejected with
  `llm.ErrDimensions` instead.
- Requesting more dimensions than the model produces is a configuration
  error.

The model name is stored with every vector (`embeddings.model`), so a model
change never mixes geometries in one index. Retrieval must filter by model.

Gemini's `embedContent` response carries no token count, so embedding spend
for a Gemini model is not metered; record a zero price and the cost stays
zero.

## Structured output

A `CompleteRequest` may carry a compiled `llm.Schema`. The provider sends it
to the API where the dialect supports it (OpenAI `response_format`, Gemini
`responseMimeType`) and always validates the returned text against it with a
full JSON Schema validator. Output that is not one JSON document, or does not
conform, is an `llm.ErrInvalidOutput` error and is never returned to the
caller. Anthropic has no tool-free schema mode, so the schema travels in the
system instruction and the same validation applies.

Schemas are compiled once by `llm.NewSchema`; an uncompilable schema is a
startup error, not a runtime surprise.

## Safety

- **No tools.** `CompleteRequest` has no field that could carry a tool
  definition, so a prompt-injected article cannot make Earnest's own models
  act. `TestNoTools` checks the type and the wire body for every provider.
- **One egress path.** Every request goes through `internal/outbound` with
  `Purpose: llm`, so the shared address checks, host spacing, redirect
  handling and private-address allowlist apply.
- **No secrets or prompts in logs.** The providers log provider, model and
  token counts at debug level only. API keys and request bodies never reach a
  log.
- **Tainted output.** Model output is data. The pipeline renders it as plain
  text and verifies citations against the cited content version before
  display; see the threat model in [research/llm.md](research/llm.md).
