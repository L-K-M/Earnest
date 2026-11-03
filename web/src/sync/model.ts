// Model is the single-user reference machine ported from
// internal/syncmodel: registers, conflict policy, clocks, range records,
// change log, generations and notifications. The vector harness and tests
// drive it as the simulated server; it performs no I/O and reads no clock.

import { ZERO_TIME } from "./time.js";
import {
  cloneChange,
  cloneRange,
  cloneResult,
  cloneState,
  deriveSource,
  emptyEntry,
  errAttempted,
  errCursorGone,
  errGeneration,
  errInvalid,
  errWithheld,
  initialState,
  MAX_QUEUE_AGE_MS,
  occurredAt,
  stateRegister,
  SyncError,
  validValue,
  type Change,
  type ChangeType,
  type Context,
  type Entry,
  type Notification,
  type ObjectKind,
  type OperationKind,
  type Pending,
  type Position,
  type RangeResult,
  type RangeWrite,
  type Register,
  type RegisterName,
  type Resources,
  type Result,
  type Revision,
  type Scope,
  type Source,
  type State,
  type Status,
  type SyncObject,
  type TokenKind,
  type Value,
  type Write,
  DONE,
  NONE,
  SEEN,
  SEEN_VALUE,
  UNSEEN,
} from "./types.js";

const stateKey = (principal: bigint, entry: bigint) => `${principal}:${entry}`;
const opKey = (actor: bigint, id: string) => `${actor}${id}`;
const objectKey = (kind: ObjectKind, id: bigint) => `${kind}:${id}`;
const positionKey = (principal: bigint, view: string) => `${principal}:${view}`;

interface remembered {
  opId: string;
  principal: bigint;
  entry: bigint;
  status: Status;
  rangeResult?: RangeResult;
}

function validateWrite(ctx: Context, w: Write, current: Revision): void {
  if (w.base.generation !== current.generation) throw errGeneration();
  if (
    ctx.actor <= 0n ||
    ctx.receipt === ZERO_TIME ||
    w.opId === "" ||
    w.principal <= 0n ||
    w.entryId <= 0n ||
    w.occurredAt === ZERO_TIME ||
    !validValue(w.register, w.value) ||
    w.base.rev > current.rev
  ) {
    throw errInvalid();
  }
  deriveSource(ctx.token, w.kind);
}

function rank(source: Source): number {
  switch (source) {
    case "deliberate":
      return 3;
    case "rule_or_agent":
      return 2;
    case "passive":
      return 1;
    default:
      return 0;
  }
}

function wins(name: RegisterName, current: Register, next: Register, base: bigint): boolean {
  if (base === current.rev) return true;
  if (name === SEEN && current.source === "passive" && next.source === "passive") return next.value === SEEN_VALUE;
  if (rank(next.source) !== rank(current.source)) return rank(next.source) > rank(current.source);
  if (next.occurredAt !== current.occurredAt) return next.occurredAt > current.occurredAt;
  return next.rev > current.rev;
}

// resolveWrite is the service's row-level rule implementation. revision is
// the allocated change revision in the current transaction. A zero Entry
// represents an absent or deleted body.
export function resolveWrite(ctx: Context, w: Write, e: Entry, s: State, revision: Revision): Result {
  if (revision.generation === "" || revision.rev === 0n) throw errInvalid();
  validateWrite(ctx, w, { generation: revision.generation, rev: revision.rev - 1n });
  if (
    s.principal !== w.principal ||
    s.entryId !== w.entryId ||
    (e.id !== 0n && e.id !== w.entryId) ||
    !validValue(SEEN, s.seen.value) ||
    !validValue("shelf", s.shelf.value) ||
    !validValue("kept", s.kept.value) ||
    s.stateRev > revision.rev ||
    s.seen.rev > s.stateRev ||
    s.shelf.rev > s.stateRev ||
    s.kept.rev > s.stateRev
  ) {
    throw errInvalid();
  }
  const rev = revision.rev;
  const r: Result = { opId: w.opId, status: "applied", state: cloneState(s), replay: false };
  if (e.id === 0n || e.deleted) {
    r.status = "gone";
    return r;
  }
  const occurred = occurredAt(w.occurredAt, ctx.receipt);
  if (ctx.receipt - occurred > MAX_QUEUE_AGE_MS) {
    r.status = "expired";
    return r;
  }
  const source = deriveSource(ctx.token, w.kind);
  const next: Register = { value: w.value, rev, occurredAt: occurred, actor: ctx.actor, source };
  const current = { ...stateRegister(r.state, w.register) };
  // The passive merge is a resolved value, including two passive unseens.
  if (w.register === SEEN && w.base.rev !== current.rev && current.source === "passive" && next.source === "passive" && current.value === UNSEEN) {
    next.value = SEEN_VALUE;
  }
  let base = w.base.rev;
  // An earlier item in this transaction may have cleared Done. That causal
  // side effect is not a concurrent device write; later batch intent applies.
  if (current.rev === rev) base = current.rev;
  if (!wins(w.register, current, next, base)) {
    r.status = "superseded";
    return r;
  }
  switch (w.register) {
    case SEEN:
      r.state.seen = next;
      // Unread is one invariant, including when the shelf write would
      // otherwise outrank it. The side effect shares the accepted action's
      // provenance.
      if (next.value === UNSEEN && r.state.shelf.value === DONE) {
        r.state.shelf = { ...next, value: NONE };
      }
      break;
    case "shelf":
      r.state.shelf = next;
      break;
    case "kept":
      r.state.kept = next;
      break;
  }
  r.state.stateRev = rev;
  r.status = "applied";
  return r;
}

export class Model {
  private generation: string;
  private rev = 0n;
  private seq = 0n;
  private floor = 0n;
  private entries = new Map<bigint, Entry>();
  private states = new Map<string, State>();
  private operations = new Map<string, remembered>();
  private log: Change[] = [];
  private objects = new Map<string, SyncObject>();
  private positions = new Map<string, Position>();
  private notifications = new Map<string, Notification>();
  private withheld = false;

  constructor(generation: string) {
    if (generation === "") throw errInvalid();
    this.generation = generation;
  }

  cursor(): Revision {
    return { generation: this.generation, rev: this.rev };
  }
  highestSeq(): bigint {
    return this.seq;
  }
  getGeneration(): string {
    return this.generation;
  }
  entry(id: bigint): Entry | undefined {
    return this.entries.get(id);
  }
  state(principal: bigint, entry: bigint): State {
    return this.states.get(stateKey(principal, entry)) ?? initialState(principal, entry);
  }
  object(kind: ObjectKind, id: bigint): SyncObject | undefined {
    return this.objects.get(objectKey(kind, id));
  }
  position(principal: bigint, view: string): Position | undefined {
    return this.positions.get(positionKey(principal, view));
  }
  notification(actor: bigint, opId: string): Notification | undefined {
    return this.notifications.get(opKey(actor, opId));
  }
  changeLog(): Change[] {
    return this.log;
  }
  retentionFloor(): bigint {
    return this.floor;
  }
  isWithheld(): boolean {
    return this.withheld;
  }

  // ingest is the commit step, after fetching and parsing. A supplied seq
  // must advance visible order; zero allocates the next sequence.
  ingest(e: Entry, receipt: number): Entry {
    if (e.id <= 0n || e.subscriptionId <= 0n || receipt === ZERO_TIME || e.deleted) throw errInvalid();
    if (this.entries.has(e.id)) throw errInvalid("entry identity reused");
    const next = { ...e };
    if (next.seq === 0n) next.seq = this.seq + 1n;
    if (next.seq <= this.seq) throw errInvalid("ingest sequence must advance");
    next.firstSeenAt = receipt;
    next.rev = this.rev + 1n;
    this.seq = next.seq;
    this.entries.set(next.id, next);
    this.append({ rev: 0n, type: "entry.created", entry: next });
    return { ...next };
  }

  updateEntry(id: bigint, published: number): void {
    const e = this.entries.get(id);
    if (e === undefined || e.deleted) throw errInvalid();
    e.publishedAt = published;
    e.rev = this.rev + 1n;
    this.entries.set(id, e);
    this.append({ rev: 0n, type: "entry.updated", entry: e });
  }

  deleteEntry(id: bigint): void {
    const e = this.entries.get(id);
    if (e === undefined || e.deleted) throw errInvalid();
    e.deleted = true;
    e.rev = this.rev + 1n;
    this.entries.set(id, e);
    this.append({ rev: 0n, type: "entry.deleted", entry: e });
  }

  private resolve(ctx: Context, w: Write, rev: bigint): Result {
    const r = resolveWrite(ctx, w, this.entries.get(w.entryId) ?? emptyEntry(), this.state(w.principal, w.entryId), {
      generation: this.generation,
      rev,
    });
    if (r.status === "applied") this.states.set(stateKey(w.principal, w.entryId), cloneState(r.state));
    return r;
  }

  private replay(key: string): Result | undefined {
    const old = this.operations.get(key);
    if (old === undefined) return undefined;
    const r: Result = { opId: old.opId, status: old.status, state: this.state(old.principal, old.entry), replay: true };
    const e = this.entries.get(old.entry);
    if (e === undefined || e.deleted) r.status = "gone";
    return r;
  }

  // batch validates every input before changing anything. Applied registers
  // share one change revision; duplicate register targets in a batch are
  // rejected.
  batch(ctx: Context, writes: Write[]): Result[] {
    const targets = new Set<string>();
    const ids = new Set<string>();
    for (let i = 0; i < writes.length; i++) {
      const w = writes[i];
      try {
        validateWrite(ctx, w, this.cursor());
      } catch (err) {
        if (err instanceof SyncError) throw new SyncError(err.code, `batch item ${i}: ${err.message}`);
        throw err;
      }
      const target = `${stateKey(w.principal, w.entryId)}|${w.register}`;
      if (targets.has(target) || ids.has(w.opId)) throw errInvalid("duplicate batch target or op_id");
      const old = this.operations.get(opKey(ctx.actor, w.opId));
      if (old !== undefined && old.rangeResult !== undefined) throw errInvalid();
      targets.add(target);
      ids.add(w.opId);
    }
    const results: Result[] = [];
    let applied = false;
    for (const w of writes) {
      const key = opKey(ctx.actor, w.opId);
      const replayed = this.replay(key);
      if (replayed !== undefined) {
        results.push(replayed);
        continue;
      }
      const r = this.resolve(ctx, w, this.rev + 1n);
      results.push(r);
      applied = applied || r.status === "applied";
      this.operations.set(key, { opId: w.opId, principal: w.principal, entry: w.entryId, status: r.status });
    }
    // Return the final effective state even when this batch changed two
    // registers.
    for (const r of results) r.state = this.state(r.state.principal, r.state.entryId);
    if (applied) this.append({ rev: 0n, type: "state.updated", results: results.map(cloneResult) });
    return results;
  }

  private subscriptions(scope: Scope): bigint[] {
    const ids = new Set<bigint>();
    switch (scope.kind) {
      case "all":
        for (const e of this.entries.values()) ids.add(e.subscriptionId);
        break;
      case "subscriptions":
        if (scope.ids.length === 0) throw errInvalid();
        for (const id of scope.ids) {
          if (id <= 0n) throw errInvalid();
          ids.add(id);
        }
        break;
      case "folder":
        if (scope.ids.length !== 1 || scope.ids[0] <= 0n) throw errInvalid();
        for (const o of this.objects.values()) {
          if (o.kind === "subscription" && !o.deleted && o.folderId === scope.ids[0]) ids.add(o.id);
        }
        break;
      default:
        throw errInvalid();
    }
    return [...ids].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  }

  // through commits a range atomically. Storage may prepare chunks, but none
  // may become visible until this result and its single event can commit
  // together.
  through(ctx: Context, request: RangeWrite): RangeResult {
    const w = { ...request.write, base: { ...request.write.base } };
    // EntryID is not a range target; use a valid sentinel for common
    // validation.
    w.entryId = 1n;
    validateWrite(ctx, w, this.cursor());
    if (w.register !== SEEN || w.value !== SEEN_VALUE) throw errInvalid();
    const subs = this.subscriptions(request.scope);
    const key = opKey(ctx.actor, w.opId);
    const old = this.operations.get(key);
    if (old !== undefined) {
      if (old.rangeResult === undefined) throw errInvalid();
      const r = cloneRange(old.rangeResult);
      r.replay = true;
      for (const result of r.results) {
        result.state = this.state(result.state.principal, result.state.entryId);
        result.replay = true;
        const e = this.entries.get(result.state.entryId);
        if (e === undefined || e.deleted) result.status = "gone";
      }
      return r;
    }
    const r: RangeResult = { status: "applied", throughSeq: request.throughSeq, subscriptions: subs, except: [], results: [], replay: false };
    if (ctx.receipt - occurredAt(w.occurredAt, ctx.receipt) > MAX_QUEUE_AGE_MS) {
      r.status = "expired";
    } else {
      const memberIds: bigint[] = [];
      for (const e of this.entries.values()) {
        if (!e.deleted && e.seq <= r.throughSeq && subs.includes(e.subscriptionId)) memberIds.push(e.id);
      }
      memberIds.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
      for (const id of memberIds) {
        const item = { ...w, base: { ...w.base }, entryId: id };
        const result = this.resolve(ctx, item, this.rev + 1n);
        r.results.push(result);
        if (result.status !== "applied") r.except.push(id);
      }
      this.append({ rev: 0n, type: "state.range", range: cloneRange(r) });
    }
    this.operations.set(key, { opId: w.opId, principal: w.principal, entry: 0n, status: r.status, rangeResult: cloneRange(r) });
    return r;
  }

  // cluster resolves current membership in the service and uses the ordinary
  // atomic batch, never a range that could include a member ingested later.
  cluster(ctx: Context, writes: Write[]): Result[] {
    return this.batch(ctx, writes);
  }

  // throughAt maps Google Reader ts; undefined uses the receipt-visible
  // highest seq.
  throughAt(ts?: number): bigint {
    if (ts === undefined) return this.seq;
    let through = 0n;
    for (const e of this.entries.values()) {
      if (e.firstSeenAt <= ts && e.seq > through) through = e.seq;
    }
    return through;
  }

  private append(change: Change): void {
    this.rev += 1n;
    change.rev = this.rev;
    this.log.push(cloneChange(change));
  }

  changes(cursor: Revision, limit: number): { changes: Change[]; next: Revision } {
    if (cursor.generation !== this.generation) throw errGeneration();
    if (cursor.rev < this.floor || cursor.rev > this.rev) throw errCursorGone();
    if (limit <= 0) throw errInvalid();
    const out: Change[] = [];
    const next: Revision = { ...cursor };
    for (const c of this.log) {
      if (c.rev <= cursor.rev) continue;
      out.push(cloneChange(c));
      next.rev = c.rev;
      if (out.length === limit) break;
    }
    return { changes: out, next };
  }

  // retainAfter discards events through rev; a cursor exactly at rev remains
  // valid.
  retainAfter(rev: bigint): void {
    if (rev < this.floor || rev > this.rev) throw errInvalid();
    this.floor = rev;
    this.log = this.log.filter((c) => c.rev > rev);
  }

  // putObject preserves removals as revisioned tombstones. Label IDs
  // identify entry-label memberships, not names shared by unrelated entries.
  putObject(o: SyncObject): SyncObject {
    if (o.id <= 0n) throw errInvalid();
    const old = this.objects.get(objectKey(o.kind, o.id));
    let kind: ChangeType;
    switch (o.kind) {
      case "subscription":
        kind = old === undefined ? "subscription.created" : "subscription.updated";
        if (o.deleted) kind = "subscription.deleted";
        break;
      case "folder":
        kind = "folder.updated";
        if (o.deleted) kind = "folder.deleted";
        break;
      case "label": {
        if (o.entryId <= 0n || o.name === "" || o.principal <= 0n) throw errInvalid();
        const e = this.entries.get(o.entryId);
        if (e === undefined || e.deleted) throw errInvalid();
        if (old !== undefined && (old.entryId !== o.entryId || old.principal !== o.principal || old.name !== o.name)) throw errInvalid();
        kind = "label.added";
        if (o.deleted) kind = "label.removed";
        break;
      }
      case "annotation":
        kind = "annotation.updated";
        if (o.deleted) kind = "annotation.deleted";
        break;
      case "token":
        if (!o.deleted) throw errInvalid();
        kind = "token.revoked";
        break;
      default:
        throw errInvalid();
    }
    const next = { ...o, rev: this.rev + 1n };
    this.objects.set(objectKey(o.kind, o.id), next);
    this.append({ rev: 0n, type: kind, object: next });
    return { ...next };
  }

  // putPosition uses receipt/commit order, not client clocks, and never
  // marks seen.
  putPosition(p: Position): Position {
    if (p.principal <= 0n || p.view === "" || p.seq > this.seq) throw errInvalid();
    const next = { ...p, rev: this.rev + 1n };
    this.positions.set(positionKey(p.principal, p.view), next);
    this.append({ rev: 0n, type: "position.updated", position: next });
    return { ...next };
  }

  // acceptNotification checks generation BEFORE deduplication, including
  // when a restored snapshot predates both acceptance and its operation
  // record.
  acceptNotification(actor: bigint, opId: string, generation: string): Notification {
    if (generation !== this.generation) throw errGeneration();
    if (actor <= 0n || opId === "") throw errInvalid();
    const key = opKey(actor, opId);
    const old = this.notifications.get(key);
    if (old !== undefined) return { ...old, replay: true };
    const n: Notification = { actor, opId, generation, status: "pending", replay: false, attemptedAt: ZERO_TIME };
    this.notifications.set(key, n);
    return { ...n };
  }

  // attemptNotification must be persisted before the transport is called. A
  // crash after this transition loses delivery rather than risking a
  // duplicate.
  attemptNotification(actor: bigint, opId: string, receipt: number): Notification {
    if (receipt === ZERO_TIME) throw errInvalid();
    const n = this.notifications.get(opKey(actor, opId));
    if (n === undefined) throw errInvalid();
    if (n.status !== "pending") throw errAttempted({ ...n });
    if (this.withheld || n.generation !== this.generation) throw errWithheld({ ...n });
    n.status = "uncertain";
    n.attemptedAt = receipt;
    this.notifications.set(opKey(actor, opId), n);
    return { ...n };
  }

  completeNotification(actor: bigint, opId: string): void {
    const key = opKey(actor, opId);
    const n = this.notifications.get(key);
    if (n === undefined || n.status !== "uncertain") throw errInvalid();
    n.status = "delivered";
    this.notifications.set(key, n);
  }

  releaseNotifications(): void {
    this.withheld = false;
  }

  clone(): Model {
    const out = new Model(this.generation);
    out.rev = this.rev;
    out.seq = this.seq;
    out.floor = this.floor;
    out.withheld = this.withheld;
    for (const [k, v] of this.entries) out.entries.set(k, { ...v });
    for (const [k, v] of this.states) out.states.set(k, cloneState(v));
    for (const [k, v] of this.objects) out.objects.set(k, { ...v });
    for (const [k, v] of this.positions) out.positions.set(k, { ...v });
    for (const [k, v] of this.notifications) out.notifications.set(k, { ...v });
    for (const [k, v] of this.operations) {
      out.operations.set(k, v.rangeResult !== undefined ? { ...v, rangeResult: cloneRange(v.rangeResult) } : { ...v });
    }
    for (const c of this.log) out.log.push(cloneChange(c));
    return out;
  }

  resources(): Resources {
    const sortBig = (a: bigint, b: bigint) => (a < b ? -1 : a > b ? 1 : 0);
    const r: Resources = {
      entries: [...this.entries.values()].map((e) => ({ ...e })),
      states: [...this.states.values()].map(cloneState),
      objects: [...this.objects.values()].map((o) => ({ ...o })),
      positions: [...this.positions.values()].map((p) => ({ ...p })),
    };
    r.entries.sort((a, b) => sortBig(a.id, b.id));
    r.states.sort((a, b) => (a.principal !== b.principal ? sortBig(a.principal, b.principal) : sortBig(a.entryId, b.entryId)));
    r.objects.sort((a, b) => (a.kind !== b.kind ? (a.kind < b.kind ? -1 : 1) : sortBig(a.id, b.id)));
    r.positions.sort((a, b) => (a.principal !== b.principal ? sortBig(a.principal, b.principal) : a.view < b.view ? -1 : a.view > b.view ? 1 : 0));
    return r;
  }

  // restore requires a unique generation supplied by the durable restore
  // process. Notification requests must retain their original generation
  // across retries.
  static restore(snapshot: Model, generation: string): Model {
    if (generation === "" || generation === snapshot.generation) throw errInvalid();
    const m = snapshot.clone();
    m.generation = generation;
    m.withheld = true;
    return m;
  }
}

export type GoogleAction = "read" | "unread" | "kept-unread" | "star" | "unstar";

// googleWrite deliberately uses a zero base: the compatibility protocol
// cannot distinguish an offline replay from a fresh action. Unread clears
// Done through the shared server rule.
export function googleWrite(generation: string, opId: string, principal: bigint, entryId: bigint, action: GoogleAction, ctx: Context): Write {
  const w: Write = {
    opId,
    principal,
    entryId,
    register: SEEN,
    value: SEEN_VALUE,
    base: { generation, rev: 0n },
    occurredAt: ctx.receipt,
    kind: "explicit",
  };
  switch (action) {
    case "read":
      w.kind = "passive";
      break;
    case "unread":
    case "kept-unread":
      w.value = UNSEEN;
      break;
    case "star":
      w.register = "kept";
      w.value = "true";
      break;
    case "unstar":
      w.register = "kept";
      w.value = "false";
      break;
    default:
      throw errInvalid();
  }
  return w;
}

export type { Pending, OperationKind, RegisterName, TokenKind, Value };
