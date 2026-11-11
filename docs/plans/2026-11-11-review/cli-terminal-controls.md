# Neutralize terminal controls in human CLI output

Written before implementation. Continuation plan: PR #166.
Baseline: `3216075` (the review plan merged over application baseline
`aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`).

## Reproduced failure

Valid JSON Feed titles and HTML text can contain terminal control characters.
Feed parsing and HTML sanitization preserve those text bytes. The CLI decodes
them from API JSON and prints them in tables and `read`, so a publisher can
emit terminal escape commands. Effects depend on the terminal; this is not
an automatic code-execution claim.

A scratch regression ran the real feed parser and sanitizer, then captured
CLI output from an HTTP fixture. Both the title table and `read` contained
literal ESC/BEL bytes. The test never sent the payload to a live terminal.
An independent LongCat refutation traced the real fetch/store/API path and
confirmed that the bytes remain reachable.

## Intended fix

- Neutralize C0/C1 and DEL controls in human-readable stdout/stderr, preserving
  newlines, tabs and ordinary Unicode. Filter cells before truncation too.
- Enforce the rendering boundary centrally rather than rely on every future
  print call remembering to escape a publisher-controlled field.
- Keep `--json` output and the explicitly raw OPML export path unchanged.
  Keep stored publisher data, identity hashes, search and API responses intact.
- Keep writer errors explicit; rendering removed bytes must not be mistaken
  for a successful short write. Do not add terminal detection or dependencies.

## Acceptance checks

1. Observe parser/sanitizer-to-CLI regressions fail before production edits.
2. Observe human table/read/import/error output neutralize unsafe controls,
   while preserving multiline text, tabs, emoji and non-Latin text.
3. Confirm JSON round-trips the original data and OPML export remains raw.
4. Test short/failing output writers and run surrounding CLI regressions.
5. Run lint, the full uncached Postgres-backed Go race suite, shell and
   packaging tests. Merge only after current-head CI and actual review.
