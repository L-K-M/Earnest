// SyncClient is the pure client-side sync engine, mirroring
// internal/syncmodel's Client with the additions a durable runtime needs:
// immutable in-flight groups retained across errors, generation-aware
// acknowledgement, and expiry reporting. It performs no I/O and reads no
// clock; the runtime drives it and persists every transition.

import type { JsonValue } from "./json.js";
import { ZERO_TIME } from "./time.js";
import {
  clonePending,
  cloneState,
  errGeneration,
  errInvalid,
  initialState,
  MAX_QUEUE_AGE_MS,
  pendingOperation,
  stateRegister,
  type Change,
  type Entry,
  type ObjectKind,
  type OperationKind,
  type Pending,
  type Position,
  type RangeResult,
  type RangeWrite,
  type RegisterName,
  type Result,
  type Revision,
  type State,
  type SyncObject,
  type Value,
  type Write,
  DONE,
  SEEN,
  SEEN_VALUE,
  SHELF,
  UNSEEN,
  validValue,
} from "./types.js";

const stateKey = (principal: bigint, entry: bigint) => `${principal}:${entry}`;
const objectKey = (kind: ObjectKind, id: bigint) => `${kind}:${id}`;
const positionKey = (principal: bigint, view: string) => `${principal}:${view}`;
const maxFlushWrites = 1000;

// OutboxEntry is one persisted outbox slot. In-flight entries are frozen: the
// exact submitted payload is retained for retries and never coalesced into.
export interface OutboxEntry {
  item: Pending;
  inflight: boolean;
}

export interface ClientSnapshot {
  v: 1;
  cursor: Revision;
  entries: Entry[];
  states: State[];
  // Unversioned gone acknowledgements hide entries until a newer backfill.
  goneEntries?: bigint[];
  objects: SyncObject[];
  positions: Position[];
  pending: OutboxEntry[];
  // bootstrapped is owned by the runtime: true only after every bootstrap
  // page merged. The engine ignores it; a snapshot missing the flag is
  // treated as needing a fresh bootstrap.
  bootstrapped?: boolean;
  // rawItems is also runtime-owned: richer bootstrap projections
  // (subscription titles, feed URLs, label definitions) the UI reads via
  // resource(). The engine ignores it.
  rawItems?: [string, JsonValue][];
}

// FlushRequest is the next outbox group to submit. Register writes go as one
// atomic batch; a range goes individually. requestGeneration binds the
// response to the generation it was sent under, so a late response from a
// superseded generation can be rejected.
export type FlushRequest = { kind: "batch"; writes: Write[]; requestGeneration: string } | { kind: "range"; range: RangeWrite; requestGeneration: string };

export interface DroppedOperation {
  opId: string;
  status: "gone" | "expired";
}

export interface AckOutcome {
  // False when the acknowledgement belonged to a superseded generation and
  // was ignored entirely: no state merged, no outbox entry removed.
  applied: boolean;
  dropped: DroppedOperation[];
}

export class SyncClient {
  private cursor: Revision;
  private entries = new Map<bigint, Entry>();
  private states = new Map<string, State>();
  private goneEntries = new Set<bigint>();
  private objects = new Map<string, SyncObject>();
  private positions = new Map<string, Position>();
  private pending: OutboxEntry[] = [];

  constructor(cursor: Revision) {
    this.cursor = { ...cursor };
  }

  static fromSnapshot(s: ClientSnapshot): SyncClient {
    const c = new SyncClient(s.cursor);
    for (const e of s.entries) c.entries.set(e.id, { ...e });
    c.goneEntries = new Set(s.goneEntries ?? []);
    for (const st of s.states) c.states.set(stateKey(st.principal, st.entryId), cloneState(st));
    for (const o of s.objects) c.objects.set(objectKey(o.kind, o.id), { ...o });
    for (const p of s.positions) c.positions.set(positionKey(p.principal, p.view), { ...p });
    c.pending = s.pending.map((e) => ({ item: clonePending(e.item), inflight: e.inflight }));
    return c;
  }

  toSnapshot(): ClientSnapshot {
    return {
      v: 1,
      cursor: { ...this.cursor },
      entries: [...this.entries.values()].map((e) => ({ ...e })),
      states: [...this.states.values()].map(cloneState),
      goneEntries: [...this.goneEntries],
      objects: [...this.objects.values()].map((o) => ({ ...o })),
      positions: [...this.positions.values()].map((p) => ({ ...p })),
      pending: this.pending.map((e) => ({ item: clonePending(e.item), inflight: e.inflight })),
    };
  }

  getCursor(): Revision {
    return { ...this.cursor };
  }

  entry(id: bigint): Entry | undefined {
    return this.entries.get(id);
  }
  object(kind: ObjectKind, id: bigint): SyncObject | undefined {
    return this.objects.get(objectKey(kind, id));
  }
  position(principal: bigint, view: string): Position | undefined {
    return this.positions.get(positionKey(principal, view));
  }
  entriesAll(): Entry[] {
    return [...this.entries.values()];
  }

  // beginBootstrap is called with the cursor read BEFORE paging resources. It
  // replaces the cache but retains local intent. Old-generation intent is
  // made stale rather than rebased onto a revision that would force it to
  // win. In-flight groups are requeued unchanged so their deduplicated
  // resubmission carries the new-generation base.
  beginBootstrap(cursor: Revision): void {
    const generationChanged = cursor.generation !== this.cursor.generation;
    if (generationChanged) {
      for (const e of this.pending) {
        const w = pendingOperation(e.item);
        w.base = { generation: cursor.generation, rev: 0n };
      }
    }
    // Receipt is uncertain after a transport failure. Same-generation
    // bootstrap must retain the submitted payload for deduplicated retry.
    if (generationChanged) for (const e of this.pending) e.inflight = false;
    this.cursor = { ...cursor };
    this.entries = new Map();
    this.states = new Map();
    this.goneEntries.clear();
    this.objects = new Map();
    this.positions = new Map();
  }

  mergeEntry(e: Entry): void {
    const old = this.entries.get(e.id);
    if (old === undefined || (e.rev > old.rev && (!old.deleted || this.goneEntries.has(e.id)))) {
      this.entries.set(e.id, { ...e });
      this.goneEntries.delete(e.id);
    }
  }

  private static mergeStateRegisters(old: State, next: State): State {
    if (next.seen.rev > old.seen.rev) old.seen = next.seen;
    if (next.shelf.rev > old.shelf.rev) old.shelf = next.shelf;
    if (next.kept.rev > old.kept.rev) old.kept = next.kept;
    if (next.stateRev > old.stateRev) old.stateRev = next.stateRev;
    return old;
  }

  private serverState(principal: bigint, entry: bigint): State {
    return this.states.get(stateKey(principal, entry)) ?? initialState(principal, entry);
  }

  mergeState(s: State): void {
    const key = stateKey(s.principal, s.entryId);
    this.states.set(key, SyncClient.mergeStateRegisters(this.serverState(s.principal, s.entryId), cloneState(s)));
  }

  mergeObject(o: SyncObject): void {
    const key = objectKey(o.kind, o.id);
    const old = this.objects.get(key);
    if (old === undefined || o.rev > old.rev) this.objects.set(key, { ...o });
  }

  mergePosition(p: Position): void {
    const key = positionKey(p.principal, p.view);
    const old = this.positions.get(key);
    if (old === undefined || p.rev > old.rev) this.positions.set(key, { ...p });
  }

  // applyPage is atomic, including its cursor. Persist cache and cursor
  // together. Replaying the same page is safe; a gap or a partial page is
  // rejected.
  applyPage(changes: Change[], next: Revision): void {
    if (next.generation !== this.cursor.generation) throw errGeneration();
    let expected = this.cursor.rev;
    for (const change of changes) {
      if (change.rev <= expected) continue;
      if (change.rev !== expected + 1n) throw errInvalid("non-contiguous change page");
      expected = change.rev;
    }
    if (next.rev !== expected) throw errInvalid("partial page with final cursor");
    for (const change of changes) {
      if (change.rev <= this.cursor.rev) continue;
      if (change.entry !== undefined) this.mergeEntry(change.entry);
      if (change.object !== undefined) this.mergeObject(change.object);
      if (change.position !== undefined) this.mergePosition(change.position);
      const results = change.range !== undefined ? change.range.results : (change.results ?? []);
      for (const r of results) {
        const e = this.entries.get(r.state.entryId);
        if (e === undefined || e.deleted) continue;
        this.mergeState(r.state);
      }
    }
    this.cursor = { ...next };
  }

  private hasOpId(id: string): boolean {
    return this.pending.some((e) => pendingOperation(e.item).opId === id);
  }

  // queue coalesces register intent per principal, entry and register,
  // keeping the oldest observed base and the newest action. Clearing Done is
  // retained as a separate shelf intent so a subsequent coalesced seen action
  // cannot resurrect Done. In-flight entries are immutable: a same-register
  // action while one is in flight appends a new operation instead.
  queue(w: Write): void {
    if (
      w.opId === "" || w.opId.endsWith("/clear-done") ||
      w.principal <= 0n ||
      w.entryId <= 0n ||
      w.occurredAt === ZERO_TIME ||
      !validValue(w.register, w.value) ||
      w.base.generation !== this.cursor.generation ||
      (w.kind !== "explicit" && w.kind !== "passive")
    ) {
      throw errInvalid();
    }
    const clearDone = w.register === SEEN && w.value === UNSEEN && this.state(w.principal, w.entryId).shelf.value === DONE;
    if (this.hasOpId(w.opId) || (clearDone && this.hasOpId(`${w.opId}/clear-done`))) throw errInvalid();
    if (clearDone) {
      const shelf = { ...w, base: { ...w.base } };
      shelf.opId = `${w.opId}/clear-done`;
      shelf.register = SHELF;
      shelf.value = "none";
      shelf.base.rev = this.serverState(w.principal, w.entryId).shelf.rev;
      this.queueWrite(shelf);
    }
    this.queueWrite({ ...w, base: { ...w.base } });
  }

  private queueWrite(w: Write): void {
    for (let i = 0; i < this.pending.length; i++) {
      const e = this.pending[i];
      if (e.inflight || e.item.kind !== "write") continue;
      const previous = e.item.write;
      if (previous.principal === w.principal && previous.entryId === w.entryId && previous.register === w.register) {
        w.base = { ...previous.base };
        this.pending.splice(i, 1);
        break;
      }
    }
    this.pending.push({ item: { kind: "write", write: w }, inflight: false });
  }

  // queueRange retains receipt-resolved boundaries, original clocks and
  // scope. It cannot expand to cached entries: uncached entries also belong
  // to a range.
  queueRange(r: RangeWrite): void {
    const w = r.write;
    if (
      w.opId === "" ||
      w.principal <= 0n ||
      w.base.generation !== this.cursor.generation ||
      w.occurredAt === ZERO_TIME ||
      w.register !== SEEN ||
      w.value !== SEEN_VALUE ||
      (w.kind !== "explicit" && w.kind !== "passive")
    ) {
      throw errInvalid();
    }
    switch (r.scope.kind) {
      case "all":
        if (r.scope.ids.length !== 0) throw errInvalid();
        break;
      case "subscriptions":
        if (r.scope.ids.length === 0) throw errInvalid();
        break;
      case "folder":
        if (r.scope.ids.length !== 1) throw errInvalid();
        break;
      default:
        throw errInvalid();
    }
    for (const id of r.scope.ids) {
      if (id <= 0n) throw errInvalid();
    }
    if (this.hasOpId(w.opId)) throw errInvalid();
    this.pending.push({
      item: { kind: "range", range: { write: { ...w, base: { ...w.base } }, scope: { kind: r.scope.kind, ids: [...r.scope.ids] }, throughSeq: r.throughSeq } },
      inflight: false,
    });
  }

  // outbox returns a deep copy of every pending item, queued and in-flight.
  outbox(): Pending[] {
    return this.pending.map((e) => clonePending(e.item));
  }

  // beginFlush freezes the next outbox group for submission: all consecutive
  // queued writes at the head form one atomic batch; a range is submitted
  // alone. Only one group may be in flight at a time so a failed submission
  // retries the exact persisted payload.
  beginFlush(): FlushRequest | null {
    if (this.pending.length === 0) return null;
    const first = this.pending[0];
    if (first.item.kind === "range") {
      first.inflight = true;
      const clone = clonePending(first.item);
      if (clone.kind !== "range") throw errInvalid();
      return { kind: "range", range: clone.range, requestGeneration: this.cursor.generation };
    }
    const writes: Write[] = [];
    const retrying = first.inflight;
    const targets = new Set<string>();
    for (const e of this.pending) {
      if (e.item.kind !== "write") break;
      if (writes.length >= maxFlushWrites) break;
      if (e.inflight !== retrying) break;
      const target = `${e.item.write.principal}:${e.item.write.entryId}:${e.item.write.register}`;
      if (targets.has(target)) break;
      targets.add(target);
      e.inflight = true;
      writes.push({ ...e.item.write, base: { ...e.item.write.base } });
    }
    return { kind: "batch", writes, requestGeneration: this.cursor.generation };
  }

  // A failed response cannot tell us whether the server committed. Keep
  // submitted slots immutable until acknowledgement or generation rotation.
  flushFailed(): void {
    // beginFlush returns this frozen group again on the next attempt.
  }

  private removeOpId(opId: string): void {
    this.pending = this.pending.filter((e) => pendingOperation(e.item).opId !== opId);
  }

  // acknowledge applies a batch response captured under requestGeneration.
  // A response from a superseded generation is ignored wholesale. Each
  // acknowledgement removes only its own operation ID, so a late response
  // cannot erase a newer replacement.
  acknowledge(requestGeneration: string, results: Result[]): AckOutcome {
    if (requestGeneration !== this.cursor.generation) return { applied: false, dropped: [] };
    const dropped: DroppedOperation[] = [];
    for (const r of results) {
      this.mergeState(r.state);
      if (r.status === "gone") {
        const e = this.entries.get(r.state.entryId);
        if (e !== undefined && !e.deleted) {
          this.goneEntries.add(e.id);
          e.deleted = true;
          this.entries.set(e.id, e);
        }
        dropped.push({ opId: r.opId, status: r.status });
      }
      if (r.status === "expired") dropped.push({ opId: r.opId, status: r.status });
      this.removeOpId(r.opId);
    }
    return { applied: true, dropped };
  }

  // acknowledgeRange adopts resolved states and removes this exact queued
  // range, under the same generation guard.
  acknowledgeRange(requestGeneration: string, opId: string, r: RangeResult): AckOutcome {
    if (requestGeneration !== this.cursor.generation) return { applied: false, dropped: [] };
    for (const result of r.results) this.mergeState(result.state);
    this.removeOpId(opId);
    const dropped: DroppedOperation[] = [];
    if (r.status === "expired" || r.status === "gone") dropped.push({ opId, status: r.status });
    return { applied: true, dropped };
  }

  // expireQueue drops queued (never in-flight) intent older than the maximum
  // queue age and reports it. Submitting such intent would only earn an
  // expired status anyway; the caller reports the drop to the user.
  expireQueue(now: number): DroppedOperation[] {
    const dropped: DroppedOperation[] = [];
    this.pending = this.pending.filter((e) => {
      if (e.inflight) return true;
      if (now - pendingOperation(e.item).occurredAt > MAX_QUEUE_AGE_MS) {
        dropped.push({ opId: pendingOperation(e.item).opId, status: "expired" });
        return false;
      }
      return true;
    });
    return dropped;
  }

  private rangeContains(r: RangeWrite, e: Entry): boolean {
    if (e.seq > r.throughSeq) return false;
    switch (r.scope.kind) {
      case "all":
        return true;
      case "subscriptions":
        return r.scope.ids.includes(e.subscriptionId);
      case "folder": {
        const o = this.objects.get(objectKey("subscription", e.subscriptionId));
        return o !== undefined && !o.deleted && o.folderId === r.scope.ids[0];
      }
    }
  }

  // state is server state plus the pending overlay.
  state(principal: bigint, entry: bigint): State {
    const s = cloneState(this.serverState(principal, entry));
    const e = this.entries.get(entry);
    if (e === undefined || e.deleted) return s;
    for (const { item } of this.pending) {
      const w = pendingOperation(item);
      if (item.kind === "range") {
        if (w.principal === principal && this.rangeContains(item.range, e)) s.seen.value = SEEN_VALUE;
        continue;
      }
      if (w.principal !== principal || w.entryId !== entry) continue;
      switch (w.register) {
        case SEEN:
          s.seen.value = w.value;
          if (w.value === UNSEEN && s.shelf.value === DONE) s.shelf.value = "none";
          break;
        case SHELF:
          s.shelf.value = w.value;
          break;
        default:
          s.kept.value = w.value;
      }
    }
    return s;
  }

  // registerBase returns the base revision a fresh intent should carry for
  // one register: the last server revision the cache observed for it.
  registerBase(principal: bigint, entry: bigint, register: RegisterName): Revision {
    return { generation: this.cursor.generation, rev: stateRegister(this.serverState(principal, entry), register).rev };
  }
}

export type { OperationKind, RegisterName, Value };
