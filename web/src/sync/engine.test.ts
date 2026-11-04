// Unit tests for the pure SyncClient: page atomicity, bootstrap replay,
// higher-revision merges, outbox coalescing, in-flight immutability,
// generation-aware acknowledgement, overlay rules, expiry and int64
// identity and per-entry tombstones in range acknowledgements.

import { describe, expect, it } from "vitest";
import { SyncClient } from "./client.js";
import {
  type Change,
  type Entry,
  type RangeWrite,
  type Result,
  type State,
  type Status,
  type Write,
  DONE,
  initialState,
} from "./types.js";

const T = Date.parse("2026-11-02T12:00:00Z");
const REV = (rev: bigint) => ({ generation: "g1", rev });

const entry = (id: bigint, seq = id): Entry => ({ id, subscriptionId: 1n, seq, firstSeenAt: T, publishedAt: T, rev: id, deleted: false });

const write = (opId: string, over: Partial<Write> = {}): Write => ({
  opId,
  principal: 1n,
  entryId: 1n,
  register: "seen",
  value: "seen",
  base: { generation: "g1", rev: 0n },
  occurredAt: T,
  kind: "explicit",
  ...over,
});

const st = (over: Partial<State> = {}): State => ({ ...initialState(1n, 1n), ...over });

const result = (opId: string, status: Status = "applied", state?: State): Result => ({ opId, status, state: state ?? st(), replay: false });

const stateChange = (rev: bigint, state: State): Change => ({ rev, type: "state.updated", results: [result("s", "applied", state)] });

const client = (): SyncClient => {
  const c = new SyncClient(REV(0n));
  return c;
};

describe("applyPage", () => {
  it("applies a contiguous page and advances the cursor", () => {
    const c = client();
    c.applyPage([{ rev: 1n, type: "entry.created", entry: entry(1n) }, stateChange(2n, st({ stateRev: 2n }))], REV(2n));
    expect(c.getCursor()).toEqual(REV(2n));
    expect(c.entry(1n)?.seq).toBe(1n);
  });
  it("rejects a gap atomically: nothing merges and the cursor stays", () => {
    const c = client();
    const page = [{ rev: 1n, type: "entry.created", entry: entry(1n) }, { rev: 3n, type: "entry.created", entry: entry(3n) }] as Change[];
    expect(() => c.applyPage(page, REV(3n))).toThrowError(/non-contiguous/);
    expect(c.entry(1n)).toBeUndefined();
    expect(c.getCursor().rev).toBe(0n);
  });
  it("rejects a partial page carrying the final cursor", () => {
    const c = client();
    c.applyPage([{ rev: 1n, type: "entry.created", entry: entry(1n) }, { rev: 2n, type: "entry.created", entry: entry(2n) }, { rev: 3n, type: "entry.created", entry: entry(3n) }], REV(3n));
    expect(() => c.applyPage([{ rev: 4n, type: "entry.created", entry: entry(4n) }], REV(5n))).toThrowError(/partial page/);
    expect(c.getCursor().rev).toBe(3n);
  });
  it("rejects a page from another generation", () => {
    const c = client();
    expect(() => c.applyPage([], { generation: "g2", rev: 0n })).toThrowError(/generation/);
  });
  it("replays the same page idempotently", () => {
    const c = client();
    const page: Change[] = [{ rev: 1n, type: "entry.created", entry: entry(1n) }, stateChange(2n, st({ stateRev: 2n }))];
    c.applyPage(page, REV(2n));
    c.applyPage(page, REV(2n));
    expect(c.getCursor().rev).toBe(2n);
  });
  it("does not create unknown or deleted entries from replayed state", () => {
    const c = client();
    c.applyPage([{ rev: 1n, type: "entry.created", entry: { ...entry(1n), deleted: true } }, stateChange(2n, st({ stateRev: 2n }))], REV(2n));
    c.applyPage([stateChange(3n, { ...st({ stateRev: 3n }), seen: { value: "seen", rev: 3n, occurredAt: T, actor: 1n, source: "deliberate" } })], REV(3n));
    expect(c.state(1n, 1n).seen.value).toBe("unseen");
    c.applyPage([stateChange(4n, { ...st({ stateRev: 4n }), seen: { value: "seen", rev: 4n, occurredAt: T, actor: 1n, source: "deliberate" } })], REV(4n));
    const s = c.state(1n, 9n);
    expect(s.seen.value).toBe("unseen");
    expect(s.entryId).toBe(9n);
  });
});

describe("merges", () => {
  it("merges entries by higher revision and keeps tombstones", () => {
    const c = client();
    c.mergeEntry({ ...entry(1n), rev: 2n, deleted: true });
    c.mergeEntry({ ...entry(1n), rev: 3n });
    expect(c.entry(1n)?.deleted).toBe(true);
  });
  it("merges registers independently by revision", () => {
    const c = client();
    c.mergeState({
      ...st({ stateRev: 5n }),
      seen: { value: "unseen", rev: 5n, occurredAt: T, actor: 1n, source: "deliberate" },
      shelf: { value: "later", rev: 2n, occurredAt: T, actor: 1n, source: "deliberate" },
    });
    c.mergeState({
      ...st({ stateRev: 4n }),
      seen: { value: "seen", rev: 4n, occurredAt: T, actor: 1n, source: "deliberate" },
      shelf: { value: DONE, rev: 4n, occurredAt: T, actor: 1n, source: "deliberate" },
    });
    const s = c.state(1n, 1n);
    expect(s.seen.value).toBe("unseen");
    expect(s.seen.rev).toBe(5n);
    expect(s.shelf.value).toBe(DONE);
    expect(s.shelf.rev).toBe(4n);
    expect(s.stateRev).toBe(5n);
  });
});

describe("outbox", () => {
  it("coalesces per principal/entry/register, keeping oldest base and newest intent", () => {
    const c = client();
    c.queue(write("w1", { base: REV(3n), occurredAt: T }));
    c.queue(write("w2", { base: REV(9n), value: "unseen", occurredAt: T + 1000 }));
    const out = c.outbox();
    expect(out).toHaveLength(1);
    const w = out[0].kind === "write" ? out[0].write : null;
    expect(w?.opId).toBe("w2");
    expect(w?.value).toBe("unseen");
    expect(w?.base).toEqual(REV(3n));
  });
  it("keeps registers independent and ordered", () => {
    const c = client();
    c.queue(write("s1", { register: "seen" }));
    c.queue(write("k1", { register: "kept", value: "true" }));
    c.queue(write("s2", { register: "seen", value: "unseen" }));
    expect(c.outbox().map((p) => (p.kind === "write" ? p.write.opId : "?"))).toEqual(["k1", "s2"]);
  });
  it("generates a shelf none intent when unread clears Done, retaining it across seen coalescing", () => {
    const c = client();
    c.mergeState({ ...st({ stateRev: 4n }), seen: { value: "seen", rev: 4n, occurredAt: T, actor: 1n, source: "deliberate" }, shelf: { value: DONE, rev: 4n, occurredAt: T, actor: 1n, source: "deliberate" } });
    c.mergeEntry(entry(1n));
    c.queue(write("u1", { value: "unseen" }));
    c.queue(write("u2", { value: "unseen", occurredAt: T + 1 }));
    const ops = c.outbox().map((p) => (p.kind === "write" ? `${p.write.register}:${p.write.opId}` : "?"));
    expect(ops).toEqual(["shelf:u1/clear-done", "seen:u2"]);
    const first = c.outbox()[0];
    const shelf = first.kind === "write" ? first.write : null;
    expect(shelf?.value).toBe("none");
    expect(shelf?.base.rev).toBe(4n);
  });
  it("freezes the in-flight group; new same-register intent appends behind it", () => {
    const c = client();
    c.queue(write("w1"));
    const req = c.beginFlush();
    expect(req?.kind).toBe("batch");
    c.queue(write("w2", { value: "unseen" }));
    const again = c.beginFlush();
    expect(again).toEqual(req);
    expect(c.outbox()).toHaveLength(2);
  });
  it("groups consecutive writes into one batch and keeps a range alone", () => {
    const c = client();
    const range: RangeWrite = { write: write("r1"), scope: { kind: "all", ids: [] }, throughSeq: 9n };
    c.queue(write("a"));
    c.queue(write("b", { entryId: 2n }));
    c.queueRange(range);
    const first = c.beginFlush();
    expect(first?.kind).toBe("batch");
    expect(first?.kind === "batch" ? first.writes.map((w) => w.opId) : []).toEqual(["a", "b"]);
    // The frozen group retries verbatim until its own acknowledgement.
    expect(c.beginFlush()).toEqual(first);
    c.acknowledge("g1", [result("a"), result("b")]);
    const second = c.beginFlush();
    expect(second?.kind).toBe("range");
  });
  it("rejects invalid writes", () => {
    const c = client();
    expect(() => c.queue(write("", {}))).toThrow();
    expect(() => c.queue(write("x", { value: "done" }))).toThrow();
    expect(() => c.queue(write("x", { base: { generation: "g2", rev: 0n } }))).toThrow();
  });
});

describe("acknowledge", () => {
  it("removes only the acknowledged op, preserving a newer replacement", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("w1"));
    c.beginFlush();
    c.queue(write("w2", { value: "unseen" }));
    const outcome = c.acknowledge("g1", [result("w1")]);
    expect(outcome.applied).toBe(true);
    expect(c.outbox()).toHaveLength(1);
    const remaining = c.outbox()[0];
    expect(remaining.kind === "write" ? remaining.write.opId : "").toBe("w2");
  });
  it("marks gone entries deleted and reports dropped operations", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("w1"));
    c.beginFlush();
    const outcome = c.acknowledge("g1", [result("w1", "gone")]);
    expect(outcome.dropped).toEqual([{ opId: "w1", status: "gone" }]);
    expect(c.entry(1n)?.deleted).toBe(true);
  });
  it("reports expired operations for user visibility", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("w1"));
    c.beginFlush();
    const outcome = c.acknowledge("g1", [result("w1", "expired")]);
    expect(outcome.dropped).toEqual([{ opId: "w1", status: "expired" }]);
    expect(c.outbox()).toHaveLength(0);
  });
  it("ignores an acknowledgement from a superseded generation entirely", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("w1"));
    c.beginFlush();
    c.beginBootstrap({ generation: "g2", rev: 0n });
    const outcome = c.acknowledge("g1", [result("w1", "applied", { ...st(), seen: { value: "seen", rev: 9n, occurredAt: T, actor: 1n, source: "deliberate" } })]);
    expect(outcome.applied).toBe(false);
    expect(outcome.dropped).toEqual([]);
    // The stale response neither merged state nor removed the rebased write.
    expect(c.outbox()).toHaveLength(1);
    const w = c.outbox()[0];
    expect(w.kind === "write" ? w.write.base : null).toEqual({ generation: "g2", rev: 0n });
    expect(c.state(1n, 1n).seen.rev).toBe(0n);
  });
  it("accepts the resubmission response under the new generation", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("w1"));
    c.beginFlush();
    c.beginBootstrap({ generation: "g2", rev: 0n });
    c.mergeEntry(entry(1n));
    const req = c.beginFlush();
    expect(req?.requestGeneration).toBe("g2");
    expect(c.acknowledge("g2", [result("w1")]).applied).toBe(true);
    expect(c.outbox()).toHaveLength(0);
  });
  it("a late response cannot erase the newer replacement it never named", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.queue(write("old"));
    c.beginFlush();
    c.queue(write("new", { value: "unseen" }));
    c.beginFlush();
    // Server answers the first submission only.
    c.acknowledge("g1", [result("old")]);
    expect(c.outbox()).toHaveLength(1);
    // The second group still flushes its own op id.
    const next = c.beginFlush();
    expect(next?.kind === "batch" ? next.writes[0].opId : "").toBe("new");
    c.acknowledge("g1", [result("new")]);
    expect(c.outbox()).toHaveLength(0);
  });
  it("guards range acknowledgements by generation too", () => {
    const c = client();
    c.queueRange({ write: write("r1"), scope: { kind: "all", ids: [] }, throughSeq: 3n });
    c.beginFlush();
    c.beginBootstrap({ generation: "g2", rev: 0n });
    const rr = { status: "applied" as Status, throughSeq: 3n, subscriptions: [], except: [], results: [], replay: false };
    expect(c.acknowledgeRange("g1", "r1", rr).applied).toBe(false);
    expect(c.outbox()).toHaveLength(1);
    expect(c.acknowledgeRange("g2", "r1", rr).applied).toBe(true);
    expect(c.outbox()).toHaveLength(0);
  });
});

describe("state overlay", () => {
  it("overlays pending register intent on server state", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.mergeState({ ...st({ stateRev: 2n }), seen: { value: "seen", rev: 2n, occurredAt: T, actor: 1n, source: "deliberate" } });
    c.queue(write("w1", { value: "unseen" }));
    expect(c.state(1n, 1n).seen.value).toBe("unseen");
  });
  it("applies pending ranges only to locally cached members within the boundary", () => {
    const c = client();
    c.mergeEntry(entry(1n, 1n));
    c.mergeEntry(entry(2n, 2n));
    c.queueRange({ write: write("r1"), scope: { kind: "all", ids: [] }, throughSeq: 1n });
    expect(c.state(1n, 1n).seen.value).toBe("seen");
    expect(c.state(1n, 2n).seen.value).toBe("unseen");
  });
  it("resolves folder scope through cached subscription objects", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.mergeObject({ kind: "subscription", id: 1n, principal: 0n, entryId: 0n, folderId: 9n, name: "s", rev: 1n, deleted: false });
    c.queueRange({ write: write("r1"), scope: { kind: "folder", ids: [9n] }, throughSeq: 5n });
    expect(c.state(1n, 1n).seen.value).toBe("seen");
    c.mergeObject({ kind: "subscription", id: 1n, principal: 0n, entryId: 0n, folderId: 9n, name: "s", rev: 2n, deleted: true });
    expect(c.state(1n, 1n).seen.value).toBe("unseen");
  });
  it("never overlays pending intent on a deleted entry", () => {
    const c = client();
    c.mergeEntry({ ...entry(1n), deleted: true });
    c.queue(write("w1"));
    expect(c.state(1n, 1n).seen.value).toBe("unseen");
  });
});

describe("bootstrap", () => {
  it("clears the cache but retains pending intent, rebased on generation rotation", () => {
    const c = client();
    c.mergeEntry(entry(1n));
    c.mergeState(st({ stateRev: 3n }));
    c.queue(write("w1", { base: REV(3n) }));
    c.beginBootstrap({ generation: "g2", rev: 0n });
    expect(c.entry(1n)).toBeUndefined();
    const w = c.outbox()[0];
    expect(w.kind === "write" ? w.write.base : null).toEqual({ generation: "g2", rev: 0n });
  });
  it("keeps the submitted payload frozen through a same-generation restart", () => {
    const c = client();
    c.queue(write("w1", { base: REV(1n) }));
    const req = c.beginFlush();
    c.beginBootstrap(REV(5n));
    expect(c.beginFlush()).toEqual(req);
  });
});

describe("expiry and identity", () => {
  it("drops queued intent older than 30 days and reports it", () => {
    const c = client();
    c.queue(write("fresh"));
    c.queue(write("stale", { entryId: 2n, occurredAt: T - 31 * 24 * 60 * 60 * 1000 }));
    const dropped = c.expireQueue(T);
    expect(dropped).toEqual([{ opId: "stale", status: "expired" }]);
    expect(c.outbox()).toHaveLength(1);
  });
  it("keeps in-flight intent even past the age bound", () => {
    const c = client();
    c.queue(write("flying", { occurredAt: T - 31 * 24 * 60 * 60 * 1000 }));
    c.beginFlush();
    expect(c.expireQueue(T)).toEqual([]);
    expect(c.outbox()).toHaveLength(1);
  });
  it("preserves int64 identity across a snapshot roundtrip", () => {
    const c = client();
    const big = 9007199254740993n;
    c.mergeEntry({ id: big, subscriptionId: 9223372036854775807n, seq: 1n, firstSeenAt: T, publishedAt: T, rev: 1n, deleted: false });
    c.queue(write("w1", { entryId: big }));
    const back = SyncClient.fromSnapshot(c.toSnapshot());
    expect(back.entry(big)?.subscriptionId).toBe(9223372036854775807n);
    const w = back.outbox()[0];
    expect(w.kind === "write" ? w.write.entryId : 0n).toBe(big);
  });
  it("roundtrips inflight markers through snapshots", () => {
    const c = client();
    c.queue(write("w1"));
    const req = c.beginFlush();
    const back = SyncClient.fromSnapshot(c.toSnapshot());
    expect(back.beginFlush()).toEqual(req);
  });
  it("reports the last server-observed register revision as the base", () => {
    const c = client();
    c.mergeState({ ...st({ stateRev: 7n }), shelf: { value: "later", rev: 7n, occurredAt: T, actor: 1n, source: "deliberate" } });
    expect(c.registerBase(1n, 1n, "shelf")).toEqual(REV(7n));
    expect(c.registerBase(1n, 1n, "seen")).toEqual(REV(0n));
  });
});

it("range acknowledgements tombstone gone entries without resurrecting or inventing them", () => {
  const c = client();
  c.mergeEntry(entry(1n));
  c.mergeEntry(entry(2n));
  c.queueRange({write: write("range", {entryId: 0n}), scope: {kind: "all", ids: []}, throughSeq: 2n});
  const response = {status: "applied" as const, throughSeq: 2n, subscriptions: [1n], except: [], replay: false,
    results: [result("range", "gone"), result("range", "applied", initialState(1n, 2n)), result("range", "gone", initialState(1n, 3n))]};
  expect(c.acknowledgeRange("old", "range", response).applied).toBe(false);
  expect(c.entry(1n)?.deleted).toBe(false);
  c.acknowledgeRange("g1", "range", response);
  expect(c.entry(1n)?.deleted).toBe(true);
  c.mergeEntry(entry(1n));
  expect(c.entry(1n)?.deleted).toBe(true);
  expect(c.entry(2n)?.deleted).toBe(false);
  expect(c.entry(3n)).toBeUndefined();
  expect(c.outbox().length).toBe(0);
});
