# Database schema

Postgres 18 with the `vector` extension. SQL lives in
`internal/store/migrations`, embedded in the binary. Run `earnest migrate`
with `EARNEST_DATABASE_URL`; `serve` also migrates before listening. Each
numbered migration commits atomically, under a session advisory lock.
`schema_migrations` records checksums: append migrations, never edit an
already deployed one. A newer database schema is rejected.

IDs are signed 64-bit integers. Times are UTC `timestamptz`; publisher dates
never order the river. Hashes are raw 32-byte SHA-256 values. Passwords are
Argon2id PHC strings. Encryption envelopes are opaque `bytea`; the service
must encrypt before writing them and never log secrets.

| Table | Purpose and choices |
| --- | --- |
| `schema_migrations` | Applied version, filename, SHA-256 checksum and time. |
| `users` | Unique username, Argon2id password hash and JSON settings. |
| `principals` | Human, agent or internal pipeline belonging to one user. One human per user; agent state remains separate. |
| `sessions` | SHA-256 session ID, random CSRF secret and expiry. No plaintext cookie ID. |
| `tokens` | SHA-256 token, app/agent/compat kind, audience, scopes, daily notification budget, expiry, last use, revocation and revision. No feed allowlist. |
| `device_codes` | Hashed device and user codes, requested scopes/audience, approval/consumption state, expiry and polling interval/last poll for RFC 8628. |
| `feeds` | URL, language, cache validators, adaptive schedule/backoff, detailed health, item counts, a fetch lease, posting rate and extraction policy/status. Public URLs deduplicate globally; private URLs only within the owner. Credentials require an owner; ownership is immutable. |
| `folders` | User-owned subscription folders, revision and tombstone. Active names are unique. |
| `subscriptions` | User/feed link, title override, same-user folder, cadence, full-text override, mute flags, initial history cutoff and first-fetch cohort boundary, revision and tombstone. New subscriptions expose only the previous day of initial history in the river; subsequent backdated arrivals still count as new by first_seen_at. A trigger rejects subscriptions to another user's private feed. |
| `entries` | Feed-local identity hash, global ingest `seq`, publisher metadata, current content hash, language, enclosures, retention tombstone, import flag and weighted search vector. Tombstones preserve identity and clear search. An imported entry came from another reader's export, keeps that reader's first sighting as `first_seen_at` and stays out of the river and editions; the first fetch that sees the same article adopts it under the fetch's identity, dates `first_seen_at` from that fetch and clears the flag. |
| `entry_revisions` | Per-user last change-log revision for a shared entry. A single revision on a shared entry cannot represent multiple users' cursors. |
| `contents` | Sanitised HTML and plain text, keyed by entry/hash/sanitizer version/source. Lists need not load bodies. |
| `entry_images` | One thumbnail per entry: the candidate image URLs chosen at ingest, fetch status, attempts and next attempt, and the stored re-encoded thumbnail with its etag. The row is the media worker's work item. Retention deletes it with the entry's contents. |
| `feed_icons` | One icon per feed: the feed's declared image and site URL used to find it, fetch status and schedule, and the stored re-encoded icon with its etag. The row is the media worker's work item. |
| `entry_state` | Per-principal seen/shelf/kept registers, each with revision/time/actor/source, plus `state_rev`. Revision zero represents an untouched register with no provenance; missing rows are unseen. |
| `positions` | Principal/view resume anchor and revision. No foreign key from anchor to entries: retention must not erase a position. Positions make no read claim. |
| `labels` | Native user labels with revision and tombstone. Agent annotation labels are separate. |
| `entry_labels` | Same-user label assignments with their own stable IDs, revision and tombstone. One label on two entries produces two membership objects. Label definitions travel in change payloads separately from membership objects. |
| `annotations` | Plain text note/label/score/brief/summary/blurb, principal provenance, source entry IDs, model, prompt version, revision and tombstone. Arrays support multi-entry briefs. |
| `changes` | Per-user ordered log, primary key `(user_id, rev)`, object payload and retention time. Range and compact batch results belong in payloads. |
| `op_ids` | Per-principal atomic operation deduplication and cached results, retained for 90 days independently of change-log retention. |
| `system` | Exactly one row containing a random database generation UUID. Rotate after restore, before serving. |
| `seq_counter` | Singleton transactional ingest counter; distinct from the nontransactional bigserial identity sequence. |
| `rev_counters` | One transactional change revision counter and retained-log floor per user, created on first allocation. The floor survives deleting every retained event. |
| `notifications` | Sender, recipient, stable operation ID, budget token, inert note, source IDs, closed category, principal-namespaced dedupe key, status/timing and generation. Attempt/delivery states require timestamps. Dedupe keys are indexed, not permanently unique: the service applies its dedupe window. |
| `devices` | User-owned ntfy/webpush/unifiedpush/apns destination, transport keys and `show_text` opt-in defaulting to false. |
| `push_settings` | User ntfy configuration with encrypted token, optional paired quiet-hour endpoints, timezone and revision. |
| `activity` | Append-only action/target/before/after/revert evidence with user and principal. Database triggers reject update/delete. Reverts append a new activity record. |
| `webhooks` | User-created destinations, encrypted signing secret, event list and active flag. A trigger requires a human creator of the same user. |
| `saved_searches` | Named query, structured filters, revision and tombstone. |
| `rules` | Source text, validated AST/action JSON, enabled flag, creator and revision. AST validation and permitted effects belong to the service. |
| `mutes` | Per-user keyword or author mute, revision and tombstone. |
| `clusters` | Per-user story grouping, revision and tombstone. |
| `entry_clusters` | Per-user cluster membership, revision and tombstone; cluster-scoped state writes resolve current entries in the service. |
| `embeddings` | Entry/model key, original dimensions and fixed `halfvec(1024)`; shorter vectors are zero-padded. HNSW uses cosine distance. Retrieval must filter model and subscription/privacy scope. |
| `llm_cache` | Content hash/kind/prompt version/model cache, output and citations. Optional owner partitions private content; NULL denotes public content. Agent text never populates this cache or embeddings. |
| `llm_spend` | Exact decimal USD spent/limit per provider and calendar month; no floating-point budgets. |
| `jobs` | Durable kind/payload, schedule, attempts, last error, lease and optional unique enqueue key. Workers claim with `FOR UPDATE SKIP LOCKED`, update the lease and commit before working. |

## Ordering and ownership

Call `store.LockCore` at the start of a domain mutation transaction,
before other row locks, when the exact allocation counts require database reads.
Then call `store.AllocateCounters` for the exact counts while retaining that
lock. Callers that already know their counts can call `store.AllocateCounters`
directly before other row locks. It locks the global seq counter first (even when
allocating zero), then per-user rev counters in ascending user ID order.
Allocate the exact counts needed, use every number, and commit the mutation,
its log events and jobs together. Abort the transaction after any error.
Rollback returns all allocations. The initial implementation serialises short
mutation commit sections globally; fetching and parsing stay outside them.

Every bootstrap resource must receive its revision from the corresponding
user's allocated change range. `entry_revisions` supplies revisions for shared
entries. Metadata tables permit revision zero during initial provisioning;
production service writes must allocate real revisions. The state conflict
policy, including clearing Done when marking unread, belongs to the service
and executable sync model, not SQL triggers.

Generation rotation atomically changes the system UUID, withholds held/queued
notifications and marks attempting sends uncertain, preserving their original
generation. Stop delivery workers during restore/rotation. The service must
reject old-generation requests, including notifications absent from the
restored backup; rotation cannot detect those replays by itself.

Scalar foreign keys and same-user composite foreign keys enforce persistent
ownership where practical. Arrays of source entry IDs intentionally survive
retention; the service validates their existence and user visibility on write.
The service also enforces scopes, human-only device/rule management, allowed
annotation kinds, source provenance, feed access in retrieval, length limits
and outbound destination policy. There is no row-level security; all feature access goes through the service.

Feed credential envelopes use AES-256-GCM with a random nonce prefixed to the
ciphertext and tag. HKDF-SHA-256 derives the key from the deployment secret
with purpose `earnest/feed-credentials/v1`; the same purpose is authenticated
as additional data. Credentials are never included in resource or sync payloads.
State provenance stores the model's `rule_or_agent` value. Reads normalize the
foundation's legacy `rule` and `agent` values to that combined class.

## Search and queue indexes

The unique seq index supports the global river; `(feed_id, seq DESC)` supports
subscription rivers. Partial state indexes cover unread, shelf and kept
lookups; absent-state unread queries must also use an anti-join. Other indexes
cover due feeds, active subscriptions by folder/cadence, bootstrap paging,
change cursors, expiry/retention, annotation source IDs/text, notification
history/dedupe/budgets, cluster membership and due jobs.

Search triggers combine title weight A with active content text weight B.
They handle body/title/language/hash/tombstone changes, prefer extracted text,
and select the highest sanitizer version within a source. Language mapping
recognises English, German, French, Spanish, Italian, Portuguese, Dutch and
Russian names or primary language tags; unknown languages use `simple`.
Queries must use the same `entry_search_config` mapping. Model-derived and
agent annotations remain separate from article search vectors.

Claim jobs in a short transaction, for example:

```sql
WITH due AS (
    SELECT id FROM jobs
    WHERE run_at <= now() AND (locked_until IS NULL OR locked_until <= now())
    ORDER BY run_at, id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE jobs SET locked_until = now() + interval '5 minutes', attempts = attempts + 1
FROM due WHERE jobs.id = due.id RETURNING jobs.*;
```

Use a stable unique key when enqueuing idempotent work. Notification delivery
is different: record attempted status before sending and never retry an
unknown outcome, even if a job lease expires.

## Configuration defaults

`config.Load` requires `EARNEST_DATABASE_URL` and a raw `EARNEST_SECRET_KEY`
of at least 32 bytes. Defaults: HTTP `:8080`, app origin
`http://localhost:8080`, separate content origin `http://localhost:8081`,
no private outbound destinations, info logging, minimum fetch interval `15m`,
maximum `24h`, retention `90` days. Intervals use Go duration syntax.
Origins must be HTTP(S), without credentials, query or fragment; they must
differ. Configure routing/TLS for the content origin before the web UI lands.
`migrate` requires only the database URL; healthcheck/version remain independent
of application secrets. Configuration formatting (including structured slog and Go-syntax formatting)
redacts both DSN and secret. Hostnames must be ASCII DNS names or IP literals;
use punycode for international names. IPv6 addresses are canonicalized, and
ambiguous legacy numeric IPv4 hosts are rejected before comparing origins.

`storetest.NewTemplate(t)` migrates one temporary database; `template.New(t)`
clones it per test, including parallel subtests. Use `DatabaseURL` for multiple
pools and `EmptyDatabaseURL` for migration tests. All are cleaned with the
parent test. Unset `EARNEST_TEST_DATABASE_URL` skips Postgres tests.
