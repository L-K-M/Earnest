# r3 review — sync model (api.md)

**Verdict:** not yet sound enough to write test vectors from. The skeleton is workable, but the range-record semantics, the batch endpoint and the precedence table leave gaps where three clients would implement three different protocols. All findings below are reasoning against the document's own claims (judgement), not external fact checks. The Postgres row-lock semantics cited in finding 4 are standard documented behaviour — the doc itself cites the same manual.

## Findings

**1. Blocking — `state.range` can't express its own conflict outcome** (api.md §Bulk, §Change feed).
Claim: mark-all "beats older deliberate unread marks and loses to newer ones" AND "clients apply the range locally" from one `state.range` feed row. Both can't hold. The server resolves per entry at write time; the feed row carries only scope + `through_seq`. A client replaying it marks every in-scope entry ≤ seq — including entries the server deliberately left unseen → silent divergence until the next write to those entries (possibly never). Second hole: the server resolves `folder`/`cluster` scope at write-time membership; the client resolves it at replay-time membership → divergence when subscriptions move folders between write and replay. Also, "a stale request can't re-mark anything" is false: absent→seen is a re-mark; the real guarantee is only "nothing above `through_seq`, loses to newer deliberate."
Proposed: the feed record must carry the resolved result — an `except` list of entry IDs the range did not mark, plus resolved member scope — or emit reaffirming `state.updated` rows for survivors.

**2. Major — `occurred_at` lower clamp inverts precedence** (api.md §State).
"Values… older than the retention bound are replaced by receipt time." A 90-day-old queued mark-all or mark-unread then looks brand-new and beats every deliberate write since — precisely the data loss the policy exists to prevent. An old `occurred_at` is the safe value: it loses ties. Clamp only the future side; ops older than log retention should keep real time or be dropped from the outbox, not promoted.

**3. Major — Google Reader writes get maximum precedence** (api.md §GR mapping).
All GR writes map to `deliberate` with `occurred_at` = receipt. A week-offline NetNewsWire pushing a 2,000-ID `edit-tag` batch lands every write at occurred_at=now, beating every deliberate native mark from that week even though many GR marks are older in intent-time. The least trustworthy clock gets the strongest precedence. Map bulk GR read-marks to a lower-precedence source, or write down the accepted loss and pin it in a vector.

**4. Major — counter rows couple ingest to interactive writes; deadlock surface** (api.md §Entry identity, §Serial revisions).
(a) The global `seq` counter serialises all ingest commits — correct for ordering, but if fetch/parse/extract live inside that transaction one slow feed stalls all ingest. Confine the counter update to a short commit-time step. (b) `entry.created` writes take every subscriber's per-user `rev` lock; two concurrent ingests on shared feeds with overlapping subscriber sets take locks in arbitrary order → deadlock. A 100k-row `state:through` also holds the user's counter, stalling that user's `entry.created` writes. Specify deterministic lock ordering and chunked range materialisation.

**5. Major — outbox writes to tombstoned entries have no defined outcome** (api.md §Outbox).
"The conflict policy handles them" — after retention deletes the entry there is no register to conflict with. Replay either resurrects state rows for dead entries or errors in an unstated way. Spec: writes referencing deleted entries get a definitive rejection and clients drop them from the outbox.

**6. Major — `state:batch`, the load-bearing offline endpoint, is undefined** (api.md §Endpoints).
Per-op `base_rev`? Atomic or per-item results? Ordering? Every outbox replay goes through it; there is nothing to write vectors against.

**7. Major — cluster scope can't deliver its claim** (api.md §Bulk).
"Resolved to member entries at write time, so a story you finished doesn't return from another feed." The duplicate arriving next week is exactly that case, and it returns. Write-time materialisation only covers already-ingested dupes; the claim needs cluster-level state or read-time suppression.

**8. Minor — precedence and read model incomplete.**
`source` has four values (`passive`, `deliberate`, `rule`, `agent`) but the conflict rules rank only two — where do rule/agent writes sit? Also undefined: what counts as unread (seen absent AND shelf none? does `done` imply seen?), position conflict rule, `op_id` scoping and dedup-hit response, "ignore events for unknown IDs" during replay, and whether a 2,000-ID `edit-tag` emits 2,000 `state.updated` rows (log bloat — needs list-valued events or synthetic range records).

## 15 test vectors

1. Mark-all while an in-scope entry holds a newer deliberate unseen → exception surfaced in feed; all clients keep it unseen.
2. 90-day-queued mark-all replays → must not beat intervening deliberate unreads.
3. Outbox write to a retention-deleted entry after re-bootstrap → definitive rejection, no resurrection.
4. Passive seen vs deliberate unseen, same entry, both stale `base_rev` → unseen wins regardless of clocks.
5. Two deliberate writes, identical `occurred_at` → higher `rev` wins, deterministically.
6. Passive seen synced after deliberate unseen → stays unseen.
7. Backdated entry ingests mid-mark-all (`seq` > `through_seq`) → remains unseen.
8. Cursor past log retention → 410 → re-bootstrap → outbox replays each op once via `op_id`.
9. Post-restore cursor (old generation) → 410, never silent divergence.
10. Cluster-scoped range, late-joining member → expected result documented (today: resurfaces).
11. Concurrent ingests on a shared feed with two subscribers → no deadlock, gapless `rev` per user.
12. 2,000-ID GR `edit-tag` after a week offline vs native deliberate unreads → whatever precedence is chosen is the tested one.
13. Bootstrap racing a range record (cursor read → range commits → snapshot) → replay restores consistency.
14. Slow ingest transaction must not expose a higher `seq` before a lower one commits.
15. `op_id` dedup hit after re-bootstrap returns current effective state and never re-enters conflict resolution.
