// Vector runner for testdata/vectors/*.json, mirroring internal/syncmodel's
// vectors_test.go op for op. Server steps drive the in-memory Model; client
// steps drive SyncClient — every client-side step in the corpus executes
// through the engine under test. Results compare by subset: expected objects
// are subsets, arrays are exact and ordered.
//
// Decoding is strict like Go's DisallowUnknownFields: unknown vector keys,
// unknown step keys and unknown ops all fail, so new vector fields cannot
// drift past this harness silently.

import { SyncClient } from "./client.js";
import { asArray, asObj, asString, intIn, isObj, parseJson, strIn, stringifyJson, type JsonObject, type JsonValue } from "./json.js";
import { googleWrite, Model, type GoogleAction } from "./model.js";
import { formatTime, parseTime, ZERO_TIME } from "./time.js";
import {
  emptyEntry,
  entryAgeSeconds,
  errGeneration,
  errInvalid,
  stateGoogleRead,
  stateUnread,
  SyncError,
  type Change,
  type Context,
  type Entry,
  type Notification,
  type OperationKind,
  type Position,
  type RangeWrite,
  type RangeResult,
  type RegisterName,
  type Revision,
  type Result,
  type State,
  type Status,
  type SyncObject,
  type TokenKind,
  type Value,
  type Write,
} from "./types.js";
import {
  decodeEntry,
  decodeObject,
  decodePosition,
  decodeScope,
  decodeState,
  decodeValue,
  encodeChange,
  encodeEntry,
  encodeNotification,
  encodeObject,
  encodePosition,
  encodeRangeResult,
  encodeResult,
  encodeRevision,
  encodeState,
} from "./wire.js";

export const VECTOR_FILES: readonly string[] = [
  "01-range-exception.json",
  "02-queue-age.json",
  "03-retention-deleted.json",
  "04-source-ranks.json",
  "05-equal-time.json",
  "06-backdated-ingest.json",
  "07-publisher-clock.json",
  "08-expired-cursor.json",
  "09-restore-generation.json",
  "10-notification-response-lost.json",
  "11-shared-ingest.json",
  "12-offline-google-batch.json",
  "13-bootstrap-range-race.json",
  "14-ingest-commit-order.json",
  "15-repeated-operation.json",
  "16-google-unread-done.json",
  "17-independent-kept.json",
  "18-late-cluster-member.json",
  "19-notification-before-snapshot.json",
  "20-google-tradeoff.json",
  "21-bootstrap-newer-range-crash.json",
  "22-native-unread-done.json",
  "23-batch-atomicity.json",
  "24-label-tombstones.json",
  "25-position-live.json",
  "26-missing-ts.json",
  "27-client-coalescing.json",
  "28-clock-boundaries.json",
  "29-resource-tombstones.json",
  "30-principal-isolation.json",
  "31-queued-range-client.json",
  "32-coalesced-done-clear.json",
  "33-restore-release-safety.json",
  "34-integer-identity.json",
  "35-batch-causal-shelf.json",
  "36-ack-out-of-order-merge.json",
  "37-ack-stale-generation.json",
  "38-independent-register-merge.json",
];

const DEFAULT_RECEIPT = Date.parse("2026-11-02T12:00:00Z");
const CHANGES_LIMIT = 100000;

const TOP_KEYS = new Set(["version", "name", "decision", "setup", "operations", "final"]);
const STEP_KEYS = new Set([
  "op",
  "server",
  "device",
  "name",
  "at",
  "actor",
  "token",
  "entry",
  "state",
  "write",
  "writes",
  "scope",
  "through_seq",
  "ts",
  "action",
  "generation",
  "cursor",
  "rev",
  "object",
  "position",
  "principal",
  "entry_id",
  "op_id",
  "count",
  "repeat",
  "expect",
  "error",
]);
const ENTRY_KEYS = new Set(["id", "subscription_id", "seq", "first_seen_at", "published_at", "rev", "deleted"]);
const WRITE_KEYS = new Set(["op_id", "principal", "entry_id", "register", "value", "base_rev", "occurred_at", "kind"]);
const SCOPE_KEYS = new Set(["kind", "ids"]);
const OBJECT_KEYS = new Set(["kind", "id", "principal", "entry_id", "folder_id", "name", "rev", "deleted"]);
const POSITION_KEYS = new Set(["principal", "view", "seq", "rev"]);
const REVISION_KEYS = new Set(["generation", "rev"]);
const STATE_KEYS = new Set(["principal", "entry_id", "seen", "shelf", "kept", "state_rev"]);
const REGISTER_KEYS = new Set(["value", "rev", "occurred_at", "actor", "source"]);

function checkKeys(o: JsonObject, allowed: Set<string>, what: string): void {
  for (const k of Object.keys(o)) {
    if (!allowed.has(k)) throw errInvalid(`${what}: unknown key ${k}`);
  }
}

export class VectorFailure extends Error {
  constructor(
    readonly vector: string,
    readonly stepIndex: number,
    readonly stepOp: string,
    message: string,
  ) {
    super(`step ${stepIndex} ${stepOp}: ${message}`);
    this.name = "VectorFailure";
  }
}

interface StepOutcome {
  result: JsonValue;
  error: string;
}

// StepWrite keeps Go's zero values: register and value may be absent until an
// op fills or rejects them.
type StepWrite = Omit<Write, "register" | "value"> & { register: RegisterName | ""; value: Value | "" };

const asWrite = (w: StepWrite): Write => w as Write;

interface Captured {
  changes: Change[];
  next: Revision;
}

type SavedResponse =
  | { kind: "write"; generation: string; results: Result[] }
  | { kind: "range"; generation: string; opId: string; result: RangeResult };

function stepState(v: JsonValue | undefined): State {
  const o = asObj(v ?? {}, "state");
  checkKeys(o, STATE_KEYS, "state");
  for (const key of ["seen", "shelf", "kept"]) {
    if (o[key] !== undefined) checkKeys(asObj(o[key], `state.${key}`), REGISTER_KEYS, `state.${key}`);
  }
  return decodeState(o);
}

const zeroNotification = (): JsonValue => ({ actor: 0n, op_id: "", generation: "", status: "", replay: false, attempted_at: formatTime(ZERO_TIME) });
const zeroRangeResult = (): JsonValue => ({ status: "", through_seq: 0n, subscriptions: null, except: null, results: null, replay: false });
const zeroObject = (): JsonValue => ({ kind: "", id: 0n, principal: 0n, rev: 0n, deleted: false });
const zeroPosition = (): JsonValue => ({ principal: 0n, view: "", seq: 0n, rev: 0n });

function stepWrite(v: JsonValue | undefined): StepWrite {
  const o = v === undefined ? {} : asObj(v, "write");
  checkKeys(o, WRITE_KEYS, "write");
  const rawValue = o.value;
  return {
    opId: strIn(o, "op_id", "write"),
    principal: intIn(o, "principal", "write"),
    entryId: intIn(o, "entry_id", "write"),
    register: strIn(o, "register", "write") as RegisterName | "",
    value: rawValue === undefined ? "" : decodeValue(rawValue),
    base: o.base_rev === undefined || o.base_rev === null ? { generation: "", rev: 0n } : revisionOf(o.base_rev, "write.base_rev"),
    occurredAt: o.occurred_at === undefined ? ZERO_TIME : parseTime(asString(o.occurred_at, "write.occurred_at")),
    kind: strIn(o, "kind", "write") as OperationKind,
  };
}

function revisionOf(v: JsonValue, what: string): Revision {
  const o = asObj(v, what);
  checkKeys(o, REVISION_KEYS, what);
  return { generation: strIn(o, "generation", what), rev: intIn(o, "rev", what) };
}

function stepEntry(v: JsonValue | undefined): Entry {
  if (v === undefined) return emptyEntry();
  const o = asObj(v, "entry");
  checkKeys(o, ENTRY_KEYS, "entry");
  return decodeEntry(v);
}

function stepObject(v: JsonValue | undefined): SyncObject {
  if (v === undefined) throw errInvalid("object required");
  checkKeys(asObj(v, "object"), OBJECT_KEYS, "object");
  return decodeObject(v);
}

function stepPosition(v: JsonValue | undefined): Position {
  if (v === undefined) throw errInvalid("position required");
  checkKeys(asObj(v, "position"), POSITION_KEYS, "position");
  return decodePosition(v);
}

function stepScope(v: JsonValue | undefined) {
  if (v === undefined) return decodeScope({});
  checkKeys(asObj(v, "scope"), SCOPE_KEYS, "scope");
  return decodeScope(v);
}

export function subsetError(want: JsonValue, got: JsonValue, path: string): string | null {
  if (isObj(want)) {
    if (!isObj(got)) return `${path}: expected object, got ${fmt(got)}`;
    for (const key of Object.keys(want)) {
      if (!(key in got)) return `${path}.${key}: missing key`;
      const err = subsetError(want[key], got[key], `${path}.${key}`);
      if (err !== null) return err;
    }
    return null;
  }
  if (Array.isArray(want)) {
    if (!Array.isArray(got) || got.length !== want.length) return `${path}: array mismatch: want ${fmt(want)}, got ${fmt(got)}`;
    for (let i = 0; i < want.length; i++) {
      const err = subsetError(want[i], got[i], `${path}[${i}]`);
      if (err !== null) return err;
    }
    return null;
  }
  if (want === got) return null;
  const numeric = (v: JsonValue): v is number | bigint => typeof v === "number" || typeof v === "bigint";
  if (numeric(want) && numeric(got) && want == got) return null;
  return `${path}: want ${fmt(want)}, got ${fmt(got)}`;
}

const fmt = (v: JsonValue): string => stringifyJson(v);

const errorName = (err: unknown): string => (err instanceof SyncError ? err.code : err instanceof Error ? err.message : String(err));

class Runner {
  private models = new Map<string, Model>();
  private clients = new Map<string, SyncClient>();
  private snapshots = new Map<string, Model>();
  private prepared = new Map<string, Entry>();
  private pages = new Map<string, Captured>();
  private responses = new Map<string, SavedResponse>();

  private model(server: string): Model {
    let m = this.models.get(server);
    if (m === undefined) {
      m = new Model("g1");
      this.models.set(server, m);
    }
    return m;
  }

  // execute returns the step result and the protocol error name, mirroring the
  // Go runner's (any, error) returns including results carried on errors.
  execute(s: JsonObject): StepOutcome {
    checkKeys(s, STEP_KEYS, "step");
    if (!("expect" in s)) throw errInvalid("step: expect required");
    const op = strIn(s, "op", "step");
    const server = strIn(s, "server", "step") || "main";
    const device = strIn(s, "device", "step");
    const name = strIn(s, "name", "step");
    const m = this.model(server);
    const ctx: Context = {
      actor: intIn(s, "actor", "step") || 1n,
      token: (strIn(s, "token", "step") || "human") as TokenKind,
      receipt: s.at === undefined ? DEFAULT_RECEIPT : parseTime(asString(s.at, "step.at")),
    };
    const principal = intIn(s, "principal", "step") || 1n;
    const entryId = intIn(s, "entry_id", "step");
    const opId = strIn(s, "op_id", "step");
    const c = device === "" ? undefined : this.clients.get(device);
    const defaults = (w: StepWrite): StepWrite => ({
      ...w,
      principal: w.principal === 0n ? principal : w.principal,
      entryId: w.entryId === 0n ? entryId : w.entryId,
      base: { ...w.base, generation: w.base.generation === "" ? m.getGeneration() : w.base.generation },
      occurredAt: w.occurredAt === ZERO_TIME ? ctx.receipt : w.occurredAt,
      kind: (w.kind || "explicit") as OperationKind,
    });
    const throughOf = (): bigint => {
      if (s.through_seq !== undefined) return intIn(s, "through_seq", "step");
      const ts = s.ts === undefined ? undefined : parseTime(asString(s.ts, "step.ts"));
      return m.throughAt(ts);
    };
    const rangeOf = (): RangeWrite => {
      const w = defaults(stepWrite(s.write));
      const register = w.register === "" ? "seen" : w.register;
      const value = w.value === "" ? "seen" : w.value;
      return { write: asWrite({ ...w, register, value }), scope: stepScope(s.scope), throughSeq: throughOf() };
    };

    switch (op) {
      case "ingest":
        return this.attempt(() => encodeEntry(m.ingest(stepEntry(s.entry), ctx.receipt)), encodeEntry(emptyEntry()));
      case "prepare_ingest":
        this.prepared.set(name, stepEntry(s.entry));
        return ok({ seq: m.highestSeq(), rev: m.cursor().rev });
      case "commit_ingest": {
        const e = this.prepared.get(name);
        if (e === undefined) return fail(null, errInvalid());
        this.prepared.delete(name);
        return this.attempt(() => encodeEntry(m.ingest(e, ctx.receipt)), encodeEntry(emptyEntry()));
      }
      case "delete":
        return this.attempt(() => {
          m.deleteEntry(entryId);
          return {};
        }, {});
      case "update_entry": {
        const published = stepEntry(s.entry).publishedAt;
        return this.attempt(() => {
          m.updateEntry(entryId, published);
          return {};
        }, {});
      }
      case "write":
        return this.attempt(() => {
          const results = m.batch(ctx, [asWrite(defaults(stepWrite(s.write)))]);
          return encodeResult(results[0]);
        }, null);
      case "batch":
      case "cluster": {
        const raw = s.writes === undefined ? [] : asArray(s.writes, "step.writes");
        const writes = raw.map((v) => asWrite(defaults(stepWrite(v))));
        return this.attempt(() => (op === "cluster" ? m.cluster(ctx, writes) : m.batch(ctx, writes)).map(encodeResult), null);
      }
      case "google": {
        const gctx: Context = { ...ctx, token: "compat" };
        return this.attempt(() => {
          const w = googleWrite(m.getGeneration(), opId, principal, entryId, strIn(s, "action", "step") as GoogleAction, gctx);
          return encodeResult(m.batch(gctx, [w])[0]);
        }, null);
      }
      case "google_batch": {
        const gctx: Context = { ...ctx, token: "compat" };
        const counts: Record<Status, number> = { applied: 0, superseded: 0, gone: 0, expired: 0 };
        try {
          const repeat = intIn(s, "repeat", "step");
          const writes: Write[] = [];
          for (let i = 0n; i < repeat; i++) {
            writes.push(googleWrite(m.getGeneration(), `${opId}-${i}`, principal, entryId + i, strIn(s, "action", "step") as GoogleAction, gctx));
          }
          const results = m.batch(gctx, writes);
          for (const r of results) counts[r.status]++;
          return ok(counts);
        } catch (err) {
          return fail(counts, err);
        }
      }
      case "repeat_ingest":
        return this.attempt(() => {
          const repeat = intIn(s, "repeat", "step");
          const base = stepEntry(s.entry);
          for (let i = 0n; i < repeat; i++) m.ingest({ ...base, id: base.id + i }, ctx.receipt);
          return { rev: m.cursor().rev, seq: m.highestSeq() };
        }, null);
      case "range":
        return this.attempt(() => encodeRangeResult(m.through(ctx, rangeOf())), zeroRangeResult());
      case "retain":
        return this.attempt(() => {
          m.retainAfter(intIn(s, "rev", "step"));
          return {};
        }, {});
      case "changes":
      case "capture_changes": {
        const cursor = revisionOf(s.cursor ?? {}, "cursor");
        if (cursor.generation === "") cursor.generation = m.getGeneration();
        try {
          const { changes, next } = m.changes(cursor, CHANGES_LIMIT);
          if (op === "capture_changes") this.pages.set(name, { changes, next });
          const types: JsonValue[] = changes.map((ch) => ch.type);
          return ok({ types, revs: changes.map((ch) => ch.rev), next: encodeRevision(next), changes: changes.map(encodeChange) });
        } catch (err) {
          return fail({ types: [], revs: [], next: { generation: "", rev: 0n }, changes: null }, err);
        }
      }
      case "checkpoint":
        this.snapshots.set(name, m.clone());
        return ok({ rev: m.cursor().rev });
      case "restore": {
        const snap = this.snapshots.get(name);
        if (snap === undefined) return fail({}, errInvalid());
        return this.attempt(() => {
          this.models.set(server, Model.restore(snap, strIn(s, "generation", "step")));
          return {};
        }, {});
      }
      case "bootstrap_begin": {
        const client = c ?? new SyncClient(m.cursor());
        if (device !== "") this.clients.set(device, client);
        client.beginBootstrap(m.cursor());
        return ok(encodeRevision(client.getCursor()));
      }
      case "bootstrap_page": {
        if (c === undefined) return fail(null, errInvalid());
        const res = m.resources();
        for (const e of res.entries) c.mergeEntry(e);
        for (const st of res.states) c.mergeState(st);
        for (const o of res.objects) c.mergeObject(o);
        for (const p of res.positions) c.mergePosition(p);
        return ok({});
      }
      case "client_pull": {
        if (c === undefined) return fail(null, errInvalid());
        let page;
        try {
          page = m.changes(c.getCursor(), CHANGES_LIMIT);
        } catch (err) {
          return fail(null, err);
        }
        try {
          c.applyPage(page.changes, page.next);
          return ok(encodeRevision(c.getCursor()));
        } catch (err) {
          return fail(encodeRevision(c.getCursor()), err);
        }
      }
      case "client_apply": {
        if (c === undefined) return fail(null, errInvalid());
        const page = this.pages.get(name);
        if (page === undefined) return fail(null, errInvalid());
        let changes = page.changes;
        if (s.count !== undefined) {
          const count = intIn(s, "count", "step");
          if (count < 0n || count > BigInt(changes.length)) return fail(null, errInvalid());
          changes = changes.slice(0, Number(count));
        }
        try {
          c.applyPage(changes, page.next);
          return ok(encodeRevision(c.getCursor()));
        } catch (err) {
          return fail(encodeRevision(c.getCursor()), err);
        }
      }
      case "client_queue": {
        if (c === undefined) return fail(null, errInvalid());
        return this.attempt(() => {
          c.queue(asWrite(defaults(stepWrite(s.write))));
          return {};
        }, {});
      }
      case "client_queue_range": {
        if (c === undefined) return fail(null, errInvalid());
        return this.attempt(() => {
          c.queueRange(rangeOf());
          return {};
        }, {});
      }
      case "client_merge_state": {
        if (c === undefined) return fail(null, errInvalid());
        c.mergeState(stepState(s.state));
        return ok({});
      }
      case "client_submit": {
        if (c === undefined) return fail(null, errInvalid());
        const pending = c.outbox()[0];
        if (pending === undefined) return fail(null, errInvalid());
        return this.attempt(() => {
          const generation = c.getCursor().generation;
          if (pending.kind === "range") {
            const result = m.through(ctx, pending.range);
            this.responses.set(name, { kind: "range", generation, opId: pending.range.write.opId, result });
            return encodeRangeResult(result);
          }
          const results = m.batch(ctx, [pending.write]);
          this.responses.set(name, { kind: "write", generation, results });
          return encodeResult(results[0]);
        }, null);
      }
      case "client_ack": {
        if (c === undefined) return fail(null, errInvalid());
        const saved = this.responses.get(name);
        if (saved === undefined) return fail(null, errInvalid());
        const outcome = saved.kind === "range"
          ? c.acknowledgeRange(saved.generation, saved.opId, saved.result)
          : c.acknowledge(saved.generation, saved.results);
        return outcome.applied ? ok({}) : fail({}, errGeneration());
      }
      case "client_flush": {
        if (c === undefined) return fail(null, errInvalid());
        return this.attempt(() => {
          const out: JsonValue[] = [];
          for (;;) {
            const req = c.beginFlush();
            if (req === null) break;
            if (req.kind === "range") {
              const result = m.through(ctx, req.range);
              c.acknowledgeRange(req.requestGeneration, req.range.write.opId, result);
              out.push(encodeRangeResult(result));
            } else {
              const results = m.batch(ctx, req.writes);
              c.acknowledge(req.requestGeneration, results);
              for (const r of results) out.push(encodeResult(r));
            }
          }
          return out;
        }, null);
      }
      case "object":
        return this.attempt(() => encodeObject(m.putObject(stepObject(s.object))), zeroObject());
      case "position":
        return this.attempt(() => encodePosition(m.putPosition(stepPosition(s.position))), zeroPosition());
      case "accept_notification": {
        const generation = strIn(s, "generation", "step") || m.getGeneration();
        return this.attempt(() => encodeNotification(m.acceptNotification(ctx.actor, opId, generation)), zeroNotification());
      }
      case "attempt_notification":
        try {
          return ok(encodeNotification(m.attemptNotification(ctx.actor, opId, ctx.receipt)));
        } catch (err) {
          const payload = err instanceof SyncError && err.payload !== undefined ? encodeNotification(err.payload as Notification) : zeroNotification();
          return { result: payload, error: errorName(err) };
        }
      case "complete_notification":
        return this.attempt(() => {
          m.completeNotification(ctx.actor, opId);
          return {};
        }, {});
      case "release_notifications":
        m.releaseNotifications();
        return ok({});
      case "assert_state": {
        let state: State;
        if (device === "") {
          state = m.state(principal, entryId);
        } else {
          if (c === undefined) return fail(null, errInvalid());
          state = c.state(principal, entryId);
        }
        return ok({ state: encodeState(state), unread: stateUnread(state), google_read: stateGoogleRead(state) });
      }
      case "assert_entry": {
        let e: Entry | undefined;
        if (device === "") {
          e = m.entry(entryId);
        } else {
          if (c === undefined) return fail(null, errInvalid());
          e = c.entry(entryId);
        }
        const entry = e ?? emptyEntry();
        return ok({ entry: encodeEntry(entry), exists: e !== undefined, age_seconds: entryAgeSeconds(entry, ctx.receipt) });
      }
      case "assert_object": {
        const probe = stepObject(s.object);
        let o: SyncObject | undefined;
        if (device === "") {
          o = m.object(probe.kind, probe.id);
        } else {
          if (c === undefined) return fail(null, errInvalid());
          o = c.object(probe.kind, probe.id);
        }
        return ok({ object: o === undefined ? zeroObject() : encodeObject(o), exists: o !== undefined });
      }
      case "assert_position": {
        const probe = stepPosition(s.position);
        let p: Position | undefined;
        if (device === "") {
          p = m.position(principal, probe.view);
        } else {
          if (c === undefined) return fail(null, errInvalid());
          p = c.position(principal, probe.view);
        }
        return ok({ position: p === undefined ? zeroPosition() : encodePosition(p), exists: p !== undefined });
      }
      case "assert_client": {
        if (c === undefined) return fail(null, errInvalid());
        return ok({ outbox_size: c.outbox().length, cursor: encodeRevision(c.getCursor()) });
      }
      case "assert_log": {
        const revs: bigint[] = [];
        const types: string[] = [];
        let gapless = true;
        let last = m.retentionFloor();
        for (const ch of m.changeLog()) {
          revs.push(ch.rev);
          types.push(ch.type);
          if (ch.rev !== last + 1n) gapless = false;
          last = ch.rev;
        }
        return ok({ rev: m.cursor().rev, seq: m.highestSeq(), generation: m.getGeneration(), revs, types, gapless, count: m.changeLog().length });
      }
      default:
        return fail(null, errInvalid(`unknown step ${op}`));
    }
  }

  private attempt(fn: () => JsonValue, fallback: JsonValue): StepOutcome {
    try {
      return ok(fn());
    } catch (err) {
      return fail(fallback, err);
    }
  }
}

const ok = (result: JsonValue): StepOutcome => ({ result, error: "" });
const fail = (result: JsonValue, err: unknown): StepOutcome => ({ result, error: errorName(err) });

// runVector decodes and executes one vector file end to end, throwing
// VectorFailure on the first mismatch.
export function runVector(fileName: string, text: string): void {
  const parsed = asObj(parseJson(text), "vector");
  checkKeys(parsed, TOP_KEYS, "vector");
  const version = intIn(parsed, "version", "vector");
  const name = strIn(parsed, "name", "vector");
  const decision = strIn(parsed, "decision", "vector");
  const final = parsed.final;
  if (version !== 1n || name === "" || decision === "" || !Array.isArray(final) || final.length === 0) {
    throw new VectorFailure(fileName, -1, "decode", "invalid vector header");
  }
  const steps: JsonObject[] = [];
  for (const section of ["setup", "operations", "final"] as const) {
    const raw = parsed[section];
    if (raw === undefined) continue;
    for (const s of asArray(raw, `vector.${section}`)) steps.push(asObj(s, `vector.${section}.step`));
  }
  const runner = new Runner();
  for (let i = 0; i < steps.length; i++) {
    const s = steps[i];
    const op = typeof s.op === "string" ? s.op : "?";
    let outcome: StepOutcome;
    try {
      outcome = runner.execute(s);
    } catch (err) {
      throw new VectorFailure(fileName, i, op, `threw ${errorName(err)}`);
    }
    const wantError = strIn(s, "error", "step");
    if (outcome.error !== wantError) {
      throw new VectorFailure(fileName, i, op, `error ${JSON.stringify(outcome.error)}, want ${JSON.stringify(wantError)}`);
    }
    const expect = s.expect;
    if (expect === undefined) throw new VectorFailure(fileName, i, op, "every step must declare expect");
    const mismatch = subsetError(expect, outcome.result, "");
    if (mismatch !== null) {
      throw new VectorFailure(fileName, i, op, `${mismatch}\ngot ${fmt(outcome.result)}`);
    }
  }
}
