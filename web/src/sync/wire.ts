// Wire encoding for the sync protocol. Integer fields are bigint end to end;
// the kept register's value crosses the wire as a JSON boolean, other
// register values as strings, matching syncmodel.Value. Decoders tolerate
// extra keys (servers may add fields) but reject malformed types.

import { asArray, asInt, asObj, asString, boolIn, intIn, isObj, strIn, type JsonObject, type JsonValue } from "./json.js";
import { formatTime, parseTime, ZERO_TIME } from "./time.js";
import {
  errInvalid,
  SyncError,
  type Change,
  type ChangeType,
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
  type Result,
  type Revision,
  type Scope,
  type ScopeKind,
  type State,
  type Status,
  type SyncObject,
  type Value,
  type Write,
} from "./types.js";

function decode<T>(v: JsonValue, what: string, f: (v: JsonValue, what: string) => T): T {
  try {
    return f(v, what);
  } catch (e) {
    if (e instanceof SyncError) throw e;
    if (e instanceof Error) throw errInvalid(`${what}: ${e.message}`);
    throw e;
  }
}

export function decodeRevision(v: JsonValue): Revision {
  const o = decode(v, "revision", asObj);
  return { generation: strIn(o, "generation", "revision"), rev: intIn(o, "rev", "revision") };
}
export const encodeRevision = (r: Revision): JsonValue => ({ generation: r.generation, rev: r.rev });

export function decodeValue(v: JsonValue): Value {
  if (v === true) return "true";
  if (v === false) return "false";
  const s = decode(v, "value", asString);
  if (s === "true" || s === "false") throw errInvalid("value: boolean must not be a string");
  return s as Value;
}
export const encodeValue = (v: Value): JsonValue => (v === "true" ? true : v === "false" ? false : v);

export function decodeRegister(v: JsonValue): Register {
  const o = decode(v, "register", asObj);
  return {
    value: decodeValue(o.value ?? null),
    rev: intIn(o, "rev", "register"),
    occurredAt: o.occurred_at === undefined ? ZERO_TIME : parseTime(asString(o.occurred_at, "register.occurred_at")),
    actor: intIn(o, "actor", "register"),
    source: strIn(o, "source", "register") as Register["source"],
  };
}
export const encodeRegister = (r: Register): JsonValue => ({
  value: encodeValue(r.value),
  rev: r.rev,
  occurred_at: formatTime(r.occurredAt),
  actor: r.actor,
  source: r.source,
});

export function decodeState(v: JsonValue): State {
  const o = decode(v, "state", asObj);
  return {
    principal: intIn(o, "principal", "state"),
    entryId: intIn(o, "entry_id", "state"),
    seen: decodeRegister(o.seen ?? {}),
    shelf: decodeRegister(o.shelf ?? {}),
    kept: decodeRegister(o.kept ?? {}),
    stateRev: intIn(o, "state_rev", "state"),
  };
}
export const encodeState = (s: State): JsonValue => ({
  principal: s.principal,
  entry_id: s.entryId,
  seen: encodeRegister(s.seen),
  shelf: encodeRegister(s.shelf),
  kept: encodeRegister(s.kept),
  state_rev: s.stateRev,
});

export function decodeEntry(v: JsonValue): Entry {
  const o = decode(v, "entry", asObj);
  return {
    id: intIn(o, "id", "entry"),
    subscriptionId: intIn(o, "subscription_id", "entry"),
    seq: intIn(o, "seq", "entry"),
    firstSeenAt: o.first_seen_at === undefined ? ZERO_TIME : parseTime(asString(o.first_seen_at, "entry.first_seen_at")),
    publishedAt: o.published_at == null ? ZERO_TIME : parseTime(asString(o.published_at, "entry.published_at")),
    rev: intIn(o, "rev", "entry"),
    deleted: boolIn(o, "deleted", "entry"),
  };
}
export const encodeEntry = (e: Entry): JsonValue => ({
  id: e.id,
  subscription_id: e.subscriptionId,
  seq: e.seq,
  first_seen_at: formatTime(e.firstSeenAt),
  published_at: formatTime(e.publishedAt),
  rev: e.rev,
  deleted: e.deleted,
});

const OBJECT_KINDS: readonly ObjectKind[] = ["subscription", "folder", "label", "annotation", "token"];

export function decodeObject(v: JsonValue): SyncObject {
  const o = decode(v, "object", asObj);
  const kind = strIn(o, "kind", "object") as ObjectKind;
  if (!OBJECT_KINDS.includes(kind)) throw errInvalid(`object.kind: ${kind}`);
  return {
    kind,
    id: intIn(o, "id", "object"),
    principal: intIn(o, "principal", "object"),
    entryId: intIn(o, "entry_id", "object"),
    folderId: intIn(o, "folder_id", "object"),
    name: strIn(o, "name", "object"),
    rev: intIn(o, "rev", "object"),
    deleted: boolIn(o, "deleted", "object"),
  };
}
export function encodeObject(o: SyncObject): JsonValue {
  const out: JsonObject = { kind: o.kind, id: o.id, principal: o.principal, rev: o.rev, deleted: o.deleted };
  if (o.entryId !== 0n) out.entry_id = o.entryId;
  if (o.folderId !== 0n) out.folder_id = o.folderId;
  if (o.name !== "") out.name = o.name;
  return out;
}

export function decodePosition(v: JsonValue): Position {
  const o = decode(v, "position", asObj);
  return {
    principal: intIn(o, "principal", "position"),
    view: strIn(o, "view", "position"),
    seq: intIn(o, "seq", "position"),
    rev: intIn(o, "rev", "position"),
  };
}
export const encodePosition = (p: Position): JsonValue => ({ principal: p.principal, view: p.view, seq: p.seq, rev: p.rev });

const REGISTERS: readonly RegisterName[] = ["seen", "shelf", "kept"];
const KINDS: readonly OperationKind[] = ["explicit", "passive"];

export function decodeWrite(v: JsonValue): Write {
  const o = decode(v, "write", asObj);
  const register = strIn(o, "register", "write") as RegisterName;
  if (!REGISTERS.includes(register)) throw errInvalid(`write.register: ${register}`);
  const kind = (strIn(o, "kind", "write") || "explicit") as OperationKind;
  if (!KINDS.includes(kind)) throw errInvalid(`write.kind: ${kind}`);
  return {
    opId: strIn(o, "op_id", "write"),
    principal: intIn(o, "principal", "write"),
    entryId: intIn(o, "entry_id", "write"),
    register,
    value: decodeValue(o.value ?? null),
    base: o.base_rev === undefined ? { generation: "", rev: 0n } : decodeRevision(o.base_rev),
    occurredAt: o.occurred_at === undefined ? ZERO_TIME : parseTime(asString(o.occurred_at, "write.occurred_at")),
    kind,
  };
}
export const encodeWrite = (w: Write): JsonValue => ({
  op_id: w.opId,
  principal: w.principal,
  entry_id: w.entryId,
  register: w.register,
  value: encodeValue(w.value),
  base_rev: encodeRevision(w.base),
  occurred_at: formatTime(w.occurredAt),
  kind: w.kind,
});

export function decodeResult(v: JsonValue): Result {
  const o = decode(v, "result", asObj);
  return {
    opId: strIn(o, "op_id", "result"),
    status: strIn(o, "status", "result") as Status,
    state: decodeState(o.state ?? {}),
    replay: boolIn(o, "replay", "result"),
  };
}
export const encodeResult = (r: Result): JsonValue => ({ op_id: r.opId, status: r.status, state: encodeState(r.state), replay: r.replay });

export function decodeScope(v: JsonValue): Scope {
  const o = decode(v, "scope", asObj);
  const kind = strIn(o, "kind", "scope") as ScopeKind;
  const raw = o.ids;
  const ids = raw === undefined || raw === null ? [] : asArray(raw, "scope.ids").map((id) => asInt(id, "scope.ids"));
  return { kind, ids };
}
export const encodeScope = (s: Scope): JsonValue => ({ kind: s.kind, ids: [...s.ids] });

export function decodeRangeWrite(v: JsonValue): RangeWrite {
  const o = decode(v, "range", asObj);
  return {
    write: decodeWrite(o.write ?? {}),
    scope: decodeScope(o.scope ?? {}),
    throughSeq: intIn(o, "through_seq", "range"),
  };
}
export const encodeRangeWrite = (r: RangeWrite): JsonValue => ({ write: encodeWrite(r.write), scope: encodeScope(r.scope), through_seq: r.throughSeq });

export function decodeRangeResult(v: JsonValue): RangeResult {
  const o = decode(v, "range_result", asObj);
  const list = (key: string): JsonValue[] => (o[key] === undefined || o[key] === null ? [] : asArray(o[key], `range_result.${key}`));
  return {
    status: strIn(o, "status", "range_result") as Status,
    throughSeq: intIn(o, "through_seq", "range_result"),
    subscriptions: list("subscriptions").map((id) => asInt(id, "range_result.subscriptions")),
    except: list("except").map((id) => asInt(id, "range_result.except")),
    results: list("results").map(decodeResult),
    replay: boolIn(o, "replay", "range_result"),
  };
}
export const encodeRangeResult = (r: RangeResult): JsonValue => ({
  status: r.status,
  through_seq: r.throughSeq,
  subscriptions: [...r.subscriptions],
  except: [...r.except],
  results: r.results.map(encodeResult),
  replay: r.replay,
});

export function decodeChange(v: JsonValue): Change {
  const o = decode(v, "change", asObj);
  const fields: JsonObject = { ...o };
  if (o.rev === undefined) fields.rev = 0n;
  if (o.type === undefined) fields.type = "";
  return changeFromFields(fields);
}

function changeFromFields(o: JsonObject): Change {
  const c: Change = { rev: intIn(o, "rev", "change"), type: strIn(o, "type", "change") as ChangeType };
  if (o.entry !== undefined && o.entry !== null) c.entry = decodeEntry(o.entry);
  if (o.results !== undefined && o.results !== null) c.results = (o.results as JsonValue[]).map(decodeResult);
  if (o.range !== undefined && o.range !== null) c.range = decodeRangeResult(o.range);
  if (o.object !== undefined && o.object !== null) c.object = decodeObject(o.object);
  if (o.position !== undefined && o.position !== null) c.position = decodePosition(o.position);
  return c;
}

// decodeChangeRecord flattens the wire record {rev, type, payload} into the
// model's Change shape; payload fields mirror syncmodel.Change fields.
export function decodeChangeRecord(v: JsonValue): Change {
  const o = decode(v, "change_record", asObj);
  const payload = o.payload === undefined || o.payload === null ? {} : asObj(o.payload, "change_record.payload");
  const fields: JsonObject = { ...payload };
  fields.rev = o.rev ?? 0n;
  fields.type = o.type ?? "";
  return changeFromFields(fields);
}

export function encodeChange(c: Change): JsonValue {
  const out: JsonObject = { rev: c.rev, type: c.type };
  if (c.entry !== undefined) out.entry = encodeEntry(c.entry);
  if (c.results !== undefined) out.results = c.results.map(encodeResult);
  if (c.range !== undefined) out.range = encodeRangeResult(c.range);
  if (c.object !== undefined) out.object = encodeObject(c.object);
  if (c.position !== undefined) out.position = encodePosition(c.position);
  return out;
}

export function decodePending(v: JsonValue): Pending {
  const o = decode(v, "pending", asObj);
  if (o.range !== undefined && o.range !== null) return { kind: "range", range: decodeRangeWrite(o.range) };
  if (o.write !== undefined && o.write !== null) return { kind: "write", write: decodeWrite(o.write) };
  throw errInvalid("pending: neither write nor range");
}
export const encodePending = (p: Pending): JsonValue => (p.kind === "write" ? { write: encodeWrite(p.write) } : { range: encodeRangeWrite(p.range) });

export function decodeNotification(v: JsonValue): Notification {
  const o = decode(v, "notification", asObj);
  return {
    actor: intIn(o, "actor", "notification"),
    opId: strIn(o, "op_id", "notification"),
    generation: strIn(o, "generation", "notification"),
    status: strIn(o, "status", "notification") as Notification["status"],
    replay: boolIn(o, "replay", "notification"),
    attemptedAt: o.attempted_at === undefined ? ZERO_TIME : parseTime(asString(o.attempted_at, "notification.attempted_at")),
  };
}
export const encodeNotification = (n: Notification): JsonValue => ({
  actor: n.actor,
  op_id: n.opId,
  generation: n.generation,
  status: n.status,
  replay: n.replay,
  attempted_at: formatTime(n.attemptedAt),
});

export function isRecord(v: JsonValue): boolean {
  return isObj(v);
}
