// Protocol types mirroring internal/syncmodel. Every 64-bit integer is a
// bigint so entry IDs and revisions survive the wire without precision loss.
// Times are epoch milliseconds (UTC); ZERO_TIME is an unset Go timestamp.

import { ZERO_TIME } from "./time.js";

export const MAX_QUEUE_AGE_MS = 30 * 24 * 60 * 60 * 1000;
export const MAX_FUTURE_SKEW_MS = 24 * 60 * 60 * 1000;

export interface Revision {
  generation: string;
  rev: bigint;
}

export type TokenKind = "human" | "agent" | "rule" | "compat";
export type OperationKind = "explicit" | "passive";
export type Source = "passive" | "rule_or_agent" | "deliberate" | "";
export type RegisterName = "seen" | "shelf" | "kept";
export type Value = "seen" | "unseen" | "none" | "later" | "done" | "true" | "false";

export const SEEN: RegisterName = "seen";
export const SHELF: RegisterName = "shelf";
export const KEPT: RegisterName = "kept";

export const UNSEEN: Value = "unseen";
export const SEEN_VALUE: Value = "seen";
export const NONE: Value = "none";
export const LATER: Value = "later";
export const DONE: Value = "done";
export const TRUE: Value = "true";
export const FALSE: Value = "false";

export type Status = "applied" | "superseded" | "gone" | "expired";
export type ChangeType =
  | "settings.updated"
  | "entry.created"
  | "entry.updated"
  | "entry.deleted"
  | "state.updated"
  | "state.range"
  | "label.added"
  | "label.removed"
  | "position.updated"
  | "subscription.created"
  | "subscription.updated"
  | "subscription.deleted"
  | "folder.updated"
  | "folder.deleted"
  | "annotation.updated"
  | "annotation.deleted"
  | "token.revoked";

export type ErrorCode = "invalid" | "generation" | "cursor_gone" | "withheld" | "attempted";

export class SyncError extends Error {
  readonly code: ErrorCode;
  readonly payload: unknown;
  constructor(code: ErrorCode, message: string, payload?: unknown) {
    super(message);
    this.name = "SyncError";
    this.code = code;
    this.payload = payload;
  }
}

export const errInvalid = (detail = "invalid sync operation") => new SyncError("invalid", detail);
export const errGeneration = () => new SyncError("generation", "database generation changed: re-bootstrap required");
export const errCursorGone = () => new SyncError("cursor_gone", "cursor outside retained log: re-bootstrap required");
export const errWithheld = (payload?: unknown) => new SyncError("withheld", "notifications withheld after restore", payload);
export const errAttempted = (payload?: unknown) => new SyncError("attempted", "notification attempt already recorded", payload);

export function errorCode(err: unknown): string {
  return err instanceof SyncError ? err.code : "";
}

export interface Context {
  actor: bigint;
  token: TokenKind;
  receipt: number;
}

// DeriveSource accepts trusted authentication and operation classification
// only. No mutation accepts a caller-supplied source.
export function deriveSource(token: TokenKind, operation: OperationKind): Source {
  if (operation !== "explicit" && operation !== "passive") throw errInvalid("unknown operation kind");
  switch (token) {
    case "agent":
    case "rule":
      return "rule_or_agent";
    case "human":
    case "compat":
      return operation === "passive" ? "passive" : "deliberate";
    default:
      throw errInvalid("unknown token kind");
  }
}

export function validValue(name: RegisterName, value: Value): boolean {
  switch (name) {
    case "seen":
      return value === UNSEEN || value === SEEN_VALUE;
    case "shelf":
      return value === NONE || value === LATER || value === DONE;
    case "kept":
      return value === FALSE || value === TRUE;
  }
}

export interface Register {
  value: Value;
  rev: bigint;
  occurredAt: number;
  actor: bigint;
  source: Source;
}

export const emptyRegister = (value: Value): Register => ({
  value,
  rev: 0n,
  occurredAt: ZERO_TIME,
  actor: 0n,
  source: "",
});

export interface State {
  principal: bigint;
  entryId: bigint;
  seen: Register;
  shelf: Register;
  kept: Register;
  stateRev: bigint;
}

// initialState represents an absent state row, including zero register revs.
export function initialState(principal: bigint, entryId: bigint): State {
  return {
    principal,
    entryId,
    seen: emptyRegister(UNSEEN),
    shelf: emptyRegister(NONE),
    kept: emptyRegister(FALSE),
    stateRev: 0n,
  };
}

export const stateUnread = (s: State) => s.seen.value === UNSEEN && s.shelf.value !== DONE;
export const stateGoogleRead = (s: State) => s.seen.value === SEEN_VALUE || s.shelf.value === DONE;

export function stateRegister(s: State, name: RegisterName): Register {
  switch (name) {
    case "seen":
      return s.seen;
    case "shelf":
      return s.shelf;
    case "kept":
      return s.kept;
  }
}

export interface Entry {
  id: bigint;
  subscriptionId: bigint;
  seq: bigint;
  firstSeenAt: number;
  publishedAt: number;
  rev: bigint;
  deleted: boolean;
}

export const emptyEntry = (): Entry => ({
  id: 0n,
  subscriptionId: 0n,
  seq: 0n,
  firstSeenAt: ZERO_TIME,
  publishedAt: ZERO_TIME,
  rev: 0n,
  deleted: false,
});

// Age is independent of the untrusted publisher timestamp. Negative age
// clamps to zero and the result saturates at Go's maximum duration in
// seconds, matching time.Duration overflow behaviour.
const MAX_DURATION_SECONDS = 9223372036.854776;

export function entryAgeSeconds(e: Entry, nowMs: number): number {
  if (nowMs < e.firstSeenAt) return 0;
  const seconds = (nowMs - e.firstSeenAt) / 1000;
  return Math.min(seconds, MAX_DURATION_SECONDS);
}

export type ObjectKind = "subscription" | "folder" | "label" | "annotation" | "token";

export interface SyncObject {
  kind: ObjectKind;
  id: bigint;
  principal: bigint;
  entryId: bigint;
  folderId: bigint;
  name: string;
  rev: bigint;
  deleted: boolean;
}

export interface Position {
  principal: bigint;
  view: string;
  seq: bigint;
  rev: bigint;
}

export interface Write {
  opId: string;
  principal: bigint;
  entryId: bigint;
  register: RegisterName;
  value: Value;
  base: Revision;
  occurredAt: number;
  kind: OperationKind;
}

export interface Result {
  opId: string;
  status: Status;
  state: State;
  replay: boolean;
}

export type ScopeKind = "all" | "subscriptions" | "folder";

export interface Scope {
  kind: ScopeKind;
  ids: bigint[];
}

export interface RangeWrite {
  write: Write;
  scope: Scope;
  throughSeq: bigint;
}

export interface RangeResult {
  status: Status;
  throughSeq: bigint;
  subscriptions: bigint[];
  except: bigint[];
  results: Result[];
  replay: boolean;
}

export interface Change {
  rev: bigint;
  type: ChangeType;
  entry?: Entry;
  results?: Result[];
  range?: RangeResult;
  object?: SyncObject;
  position?: Position;
}

// Pending is a tagged outbox item: exactly one of write or range is present.
export type Pending = { kind: "write"; write: Write } | { kind: "range"; range: RangeWrite };

export const pendingOperation = (p: Pending): Write => (p.kind === "write" ? p.write : p.range.write);

export type NotificationStatus = "pending" | "uncertain" | "delivered";

export interface Notification {
  actor: bigint;
  opId: string;
  generation: string;
  status: NotificationStatus;
  replay: boolean;
  attemptedAt: number;
}

export interface Resources {
  entries: Entry[];
  states: State[];
  objects: SyncObject[];
  positions: Position[];
}

// occurredAt clamps future clocks without promoting an old queued action.
export function occurredAt(client: number, receipt: number): number {
  if (client - receipt > MAX_FUTURE_SKEW_MS) return receipt;
  return Math.min(client, receipt);
}

export function cloneState(s: State): State {
  return { ...s, seen: { ...s.seen }, shelf: { ...s.shelf }, kept: { ...s.kept } };
}
export function cloneWrite(w: Write): Write {
  return { ...w, base: { ...w.base } };
}
export function clonePending(p: Pending): Pending {
  if (p.kind === "write") return { kind: "write", write: cloneWrite(p.write) };
  return { kind: "range", range: { write: cloneWrite(p.range.write), scope: { ...p.range.scope, ids: [...p.range.scope.ids] }, throughSeq: p.range.throughSeq } };
}
export function cloneResult(r: Result): Result {
  return { ...r, state: cloneState(r.state) };
}
export function cloneRange(r: RangeResult): RangeResult {
  return {
    ...r,
    subscriptions: [...r.subscriptions],
    except: [...r.except],
    results: r.results.map(cloneResult),
  };
}
export function cloneChange(c: Change): Change {
  return {
    ...c,
    entry: c.entry ? { ...c.entry } : undefined,
    results: c.results?.map(cloneResult),
    range: c.range ? cloneRange(c.range) : undefined,
    object: c.object ? { ...c.object } : undefined,
    position: c.position ? { ...c.position } : undefined,
  };
}
