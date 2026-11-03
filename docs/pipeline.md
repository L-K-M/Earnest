# Internal pipeline

The internal pipeline processes stored publisher titles and sanitized plain text.
It never reads agent annotations. External agents' notes remain separate,
labelled full-text search hits and receive no vector rank contribution.

Every model request goes through the LLM router, provider budget and outbound
client. Completion requests have a strict JSON schema and no tools. Model output
is untrusted data: classification can affect only the actions permitted by the
rule predicate table. Webhooks cannot depend on classification or similarity,
even when a classifier claims to have authorized one.

## Enable it

All tasks are disabled by default. Configure a provider and the `embed` or
`classify` task as described in [LLM providers](llm-providers.md). For example,
with an already configured provider called `local`:

```sh
EARNEST_LLM_TASK_EMBED_PROVIDER=local
EARNEST_LLM_TASK_EMBED_MODEL=bge-m3
EARNEST_LLM_TASK_CLASSIFY_PROVIDER=local
EARNEST_LLM_TASK_CLASSIFY_MODEL=claude-haiku-5-5
```

Choose a classification model actually served by that provider. Local endpoints
also need the explicit private-address outbound allowance documented there.
Remove both provider and model settings for a task to disable it. The server
starts the pipeline worker only if at least one task is enabled. It checks for
work every second, processing bounded batches rather than launching one model
call per feed goroutine. A database advisory lock prevents overlapping workers
from spending twice on the same batch.

`GET /v1/pipeline` reports each task as `enabled` or `disabled`. Hybrid search and
similarity endpoints return `status: disabled` when embeddings are disabled,
and `status: paused` when a required embedding or provider budget is unavailable.
Ordinary full-text search continues to work without model configuration.

Credentialled feeds default to no provider access, including local providers.
The owning human can read the setting at `GET /v1/feeds/{id}/llm` and explicitly
opt in with `PUT /v1/feeds/{id}/llm` and `{"allowed":true,"op_id":"..."}`.
Agents cannot change it. Opting out excludes existing vectors and classifier
results from semantic retrieval and rule predicates immediately. Private cache
keys and clusters are partitioned by their owning user.

## Embeddings and clusters

The worker backfills new and updated entries in sequence order. Input is title
plus sanitized text, capped at 2,048 UTF-8 bytes. This conservatively bounds the
input at 2,048 tokens even for byte-fallback tokenizers; it does not assume an
English characters-per-token ratio. Vectors record their model, source dimensions,
content hash and actual input hash. A changed title invalidates the vector too.
Current results make another batch a no-op. Changing models causes a backfill;
vectors from different models are never compared.

Clusters belong to a user. Recent entries join through an exact canonical URL,
a near-duplicate title, or cosine similarity of at least 0.88 within seven days of server ingest time. Backfill processing time never makes
a historical story recent again.
A cluster accepts at most two entries from a single feed. A stable order and
tie-break resolve multiple candidates, and membership records its join reason.
Clustering itself makes no model calls. It runs with the embedding pipeline;
private feeds without provider consent can still use deterministic matching.

Use `GET /v1/entries?group=cluster` for one representative per cluster in the page and `also_in`
counts. Counts include other subscribed members outside the page. The plain chronological view remains available. A cluster-scoped Done
still writes only its current members as an atomic state batch. A late member's
stored state stays unread; river suppression hides it after the cluster decision
without inventing a state mutation or replaying a cluster range. Migration 0006
reconstructs existing cluster decisions from their transaction receipts, including
a later cluster reopen. Explicitly
marking a member unread permits it back into the river.

## Search and rules

`GET /v1/search?q=...&mode=hybrid` combines full-text and vector candidates using
reciprocal rank fusion, with constant 60 and at most 200 candidates per side.
The result is ordered by fused rank, then sequence and annotation ID for stable
pagination. Agent notes keep separate result identities and labels. This is a
bounded retrieval window; use full-text search to page beyond it.

`GET /v1/entries/{id}/similar` uses an already stored vector. Reading an entry
never sends its body to a model or fetches its URL. A missing source vector
reports paused until backfill completes.

Classification runs only with the `classify` task enabled. The versioned prompt
is `classifier-v1`. Its small vocabulary is:

| Label | Meaning |
| --- | --- |
| `press_release` | An announcement issued by the subject or its representatives |
| `news` | Reporting a recent event |
| `opinion` | Commentary or an argument |
| `tutorial` | Instructions for doing something |
| `research` | Research findings or a technical study |
| `review` | An evaluation of a product, work or service |
| `event` | An event announcement or schedule |

Unknown labels are discarded. Structural schema violations are errors, never
instructions. Results are cached by bounded content hash, prompt version, model
and private owner. The rule evaluator uses only the current content's results.
Classifier and semantic predicates obey the existing action limits, including
feed-scoped shelf changes and the ban on model-derived webhooks. A disabled
model predicate disables its whole rule, including a negated predicate.

## Watch rules and costs

Create a watch at `POST /v1/watch-rules` with `name`, `description`, `enabled`,
`budget` (daily notifications), `check_llm`, and `check_budget` (daily confirmation
calls). List at `GET /v1/watch-rules`; delete at
`DELETE /v1/watch-rules/{id}`. A watch is a semantic rule with one notify action.
The generic rule editor cannot turn it into a shelf, label or webhook action.

Descriptions are embedded once per user, description and embedding model.
Article vectors are compared at cosine threshold 0.80. An optional confirmation
uses the `classify` route and returns only a schema-validated boolean. Its daily
call budget defaults to three and is separate from the notification budget.
Attempts are reserved before calling the model to avoid repeat spending after a
crash. Confirmation budgets count UTC days. An uncertain call is not automatically repeated. Notification provenance
names the rule and a bounded passage from the matched publisher body. Existing
quiet hours, per-rule daily limits, deduplication and delivery gates still apply.

Clustering and more-like-this are free after embeddings. Hybrid queries and new
watch descriptions cost an embedding call; classifier misses cost one small
completion. Optional watch confirmation costs at most `check_budget` attempts
per day and rule. Provider monthly caps apply to all calls, including backfill.
Exhaustion leaves work pending and pauses affected rule jobs without incrementing
their failure count or disabling the rule. Raise the cap or wait for the next
budget period to resume. Exact costs depend on configured prices and actual
provider usage; the spend table records the billed token counts. No model or
quality benchmark is implied by these defaults.

## Lazy summaries and blurbs

`POST /v1/entries/{id}/summary` generates three cited sentences on demand;
`?kind=blurb` generates a single cited line. Configure the `summary` and `blurb`
routes independently. An unconfigured task returns a 409 problem and makes no
provider call. Budget exhaustion returns a paused 503 problem. Reading ordinary
entry content never generates a summary.

Each result carries its model, prompt version, stored publisher content hash,
source (`feed` or `extracted`), sanitizer version, passages and `untrusted: true`.
Claims whose quotations fail verification stay visible with `verified: false`.
Whitespace normalization proves passage existence only, not support for a claim.
Render every generated field as plain text, without Markdown, HTML, autolinks or
images. The source article remains the authority. HTTP(S) URLs in claims must
also occur as complete URL tokens in the source.

Results are shared only for identical stored plain text, prompt version, model
and kind; private results additionally belong to their owning user. This uses
the SHA-256 of the actual selected text because a feed's content hash can name
both a feed and extracted representation. Cluster membership never supplies a
cache key. Each entry receives its own annotation under the Summaries pipeline
principal, with model, prompt version and source provenance. Updating its
content replaces that annotation and emits an annotation change.

The fetcher marks summary-only bodies as `ContentFromFeedExcerpt`; ingest stores
them as feed content with `contents.excerpt_only`. Summary-only feed bodies are
excerpts, full feed content and successful extraction are not. Excerpt results
carry: “The feed gave an excerpt only. This summary covers the excerpt.” Existing
content predating this metadata remains unknown and defaults to full content;
refetching corrects it. A publisher changing the representation can also update
this flag without changing its content hash.

## Editions

`POST /v1/editions` takes `period: daily|weekly`, `op_id`, and optionally a future
`ready_at` (at most seven days ahead). It snapshots entries ingested over the
previous day or week from This week subscriptions. Sections follow folders;
entries without a folder use This week. Cluster members in a section appear
under one publisher title with `also_in` IDs. The deterministic list includes
every selected entry and exists even before any model is configured.

`GET /v1/editions/latest` reads the latest stored paper without generating text.
With the edition task disabled it returns only the deterministic list. A trigger
with the edition task enabled enqueues work in the existing durable `jobs`
table. A database advisory lock prevents overlapping edition workers, including
when a slow call outlives its job lease. The server runs this worker alongside
rule and webhook jobs. This is an
explicit trigger API; a daily or weekly automation can call it with its own
operation ID. No recurring user preference is implied.

Prose covers up to eight representative articles per section, each capped at
4,096 UTF-8 bytes; the full deterministic list remains available. Each selected
article independently warms the configured summary and blurb caches and gets
its pipeline annotation. Only raw publisher text enters the edition prompt.
Citations refer to the snapshotted content version, even after publisher edits.
Private entries without consent stay in the human-readable list but are never
sent to a provider. Consent is rechecked on each worker pass.

A future deadline submits an OpenAI-compatible batch early, then polls once a
minute. The transport uses the same router, provider budget and outbound client,
with strict schema requests and no tools. Unsupported batch endpoints and other
provider dialects wait for the deadline and use ordinary synchronous calls.
Cancellation is requested before a synchronous fallback. A failed cancellation
is flagged on the ready paper; the provider may still finish and charge for the
batch as well as the synchronous call. Changed source permissions or models
invalidate batch results. Batch submissions conservatively charge the full-price
byte-fallback input and maximum output estimate before submission; no refund is
made for lower actual usage or cancellation. This protects the budget at the
cost of overstating batch spend. Normal synchronous calls use reported usage.

Ready papers emit `edition.ready` transactionally through the existing webhook
queue; its payload contains only the edition ID. Exhausted budgets retain jobs
as paused without consuming failure attempts. Three definite generation failures
leave a failed paper with the original list and an explicit error.

## Compiled rules and ask

`POST /v1/rules:compile` accepts `{"text":"..."}` from a human. Its only model
inputs are those words and consenting feed names framed as untrusted data. It
never sends article bodies, annotations or credentials. The model returns a
closed AST and actions. Exact feed-name resolution rejects unknown or ambiguous
names; `feeds_to_confirm` makes each resolved ID inspectable. The existing rule
validator enforces the predicate table, ownership and feed scope, followed by a
seven-day dry run and “Would have matched N items”. Nothing is saved. Review the
predicate, actions and feed IDs, then use the ordinary rules endpoint to save.
Webhook compilation additionally requires feed scope and literal keywords from
your text; a model cannot invent a label or destination ID you never wrote.

`POST /v1/ask` accepts `{"question":"..."}`. It uses the existing reciprocal-rank
fusion and vector search when embeddings are enabled, otherwise publisher-only
full-text search. Agent notes never enter either candidate list or the model
payload. It selects at most eight consenting sources, each a contiguous passage
of at most 4,096 bytes near the question's words. Answers cite those exact stored
versions. Unknown versions or fabricated passages flag the claim rather than
removing it. No retrieval hits returns empty sources and claims without an
answering call. Twenty attempts per user and UTC day are reserved atomically
before retrieval, including embedding spend. Failed or interrupted attempts
retain their reservation. Provider monthly caps apply too.

## Golden evaluation

`testdata/golden/items.json` contains 30 original, hand-written articles with
required facts, forbidden claims and classifier labels. Cases cover excerpts,
longer prose, lists, code, dates, uncertainty, URLs and injection attempts. The
link rule is mandatory for every item: no generated URL absent from the source.

Run the configured provider using the production router, schemas and budgets:

```sh
earnest eval --task summary
earnest eval --task classify
earnest eval --task edition
```

`--corpus path` selects another corpus. The command needs the usual server
configuration and database so caches and budgets are real. It prints JSON with
per-case results, rubric pass rates and citation existence rates, then exits
nonzero for a failed case, disabled task or paused budget. Classification has
no citation rate. Edition evaluation treats each item as a short independent
section. Rubrics are case-insensitive textual checks, not an automated semantic
entailment judge; passing them is evidence against these cases, not proof of
factual support. Fake-provider tests exercise the harness, while a real-provider
quality baseline must be run with your chosen model. Change the relevant prompt
version whenever changing a prompt or output contract.
