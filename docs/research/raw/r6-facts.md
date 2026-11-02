# Round 9 review: external facts

Reviewer: SWE-2 max. Verifier: GLM-5.3, asked to refute each finding. The report is unedited; the verdicts follow it.

ID: FACT-1
Severity: minor
Location: docs/research/tech-stack.md:59
Issue: The doc states sqlite-vec has "no ANN index," but the cited v0.1.10 alpha series added ANN indexes, so the claim is wrong as written.
Evidence: tech-stack.md:59 "sqlite-vec is still `v0.1.10-alpha.4`, pre-v1, with no ANN index, so brute force over 5.5M vectors"; https://github.com/asg017/sqlite-vec/releases — v0.1.10-alpha.1 notes: "Initial alpha release of `sqlite-vec` with new ANN indexes: rescore, ivf (experimental, not enabled), and DiskANN," and v0.1.10-alpha.4 fixes bugs in "the new ivf/diskann features."
Fix: Reword to say sqlite-vec's ANN indexes exist only in pre-release form and are experimental/off by default, so it is still effectively brute force for a stable deployment.
ID: FACT-2
Severity: minor
Location: docs/research/api.md:252
Issue: "Mutating tools take a stable `operation_id`" appears in a list introduced as what a server must do under MCP revision 2026-07-28, but the spec defines no `operation_id` field or requirement — it is Earnest's own application-level idempotency key.
Evidence: api.md:243 "What a server must do:" + api.md:252 "Mutating tools take a stable `operation_id`; a new JSON-RPC ID can't prevent a duplicate notification"; the 2026-07-28 changelog, schema, tools, transports, and security-best-practices pages contain no `operation_id`, and the changelog says a retried request is re-issued with a new JSON-RPC request ID.
Fix: Mark `operation_id` as Earnest's own tool-argument convention for deduplication rather than something the MCP specification provides.

## Verification

- **FACT-1: CONFIRMED.** tech-stack.md:59 says sqlite-vec has no ANN index, but the project's own releases page (https://github.com/asg017/sqlite-vec/releases) announces v0.1.10-alpha.1 as "Initial alpha release of sqlite-vec with new ANN indexes: rescore, ivf (experimental, not enabled), and DiskANN" and alpha.2/alpha.4 fix bugs in the flat/ANN indexes; the error is inherited from raw/r2-stack.md:41 and is not recorded in validation.md.
- **FACT-2: CONFIRMED.** api.md:252 places "Mutating tools take a stable `operation_id`" inside the list introduced at api.md:243 as "What a server must do" under revision 2026-07-28, yet the 2026-07-28 changelog, transports page and schema.json contain zero occurrences of operation_id/operationId (the changelog instead says a retried request is re-issued with a new request ID), and validation.md does not record this.
