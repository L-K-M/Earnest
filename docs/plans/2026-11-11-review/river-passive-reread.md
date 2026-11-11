# Preserve later reading after an explicit unread

Written before implementation, following PR #166.
Application review baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.

## Confirmed path

River retains row IDs in its passive-seen deduplication set for the whole
view mount. A later explicit unread can change current state, but re-opening
or genuinely passing the row again is dropped by the retained ID. Opening
the reader does not remount River. The gate also retains rows that were
already seen, so unread arriving from the toolbar or another client fails too.
An independent LongCat refutation confirmed the caller/mount path and the
current-base sync rule: a new reading event is not a stale passive override.

## Planned change and checks

- Reproduce a settled passive read followed by unread and a later re-open,
  plus an initially-seen row later made unread, before changing production.
- Use the set only to deduplicate writes currently in flight. Never retain
  already-seen rows; release an ID on settlement. Keep a later pending event
  queued while its earlier write is outstanding.
- Preserve the existing explicit-unread rule: drop a passive mark that was
  queued before the unread. Do not mark a row seen merely because it renders
  or because an unread is delivered while its reader remains open.
- Cover keyboard/menu unread, externally updated state, re-open/scroll,
  in-flight intent and failure retry. Keep sync conflict and outbox rules intact.
- Run targeted regressions before/after, then full web typecheck, lint, tests,
  build and generated-client validation. CI/review must pass before merge.

## Review round 1: preserve the age of failed intent

A failed passive enqueue can be retried after a newer unread arrives. That
retry must not obtain a fresh base and overwrite the newer unread. Reproduce
this before changing the retry path. Stamp pending intent with the seen
revision observed at the actual reading event and discard it if that revision
changes. Retain the queued intent object while it is in flight so local unread
can cancel it and an old callback cannot overwrite a newer reading event.
No change to sync conflict policy or the ReaderActions API is needed.
