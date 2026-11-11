// ReaderSync integration tests over a Model-backed transport. The fake
// request layer serves the exact wire shapes the service emits and forwards
// mutations to the in-memory server, so flush/pull exercise the real decode
// and acknowledge paths.

import { describe, expect, it } from "vitest";
import { asArray, asObj, parseJson, stringifyJson, type JsonValue } from "./json.js";
import { Model } from "./model.js";
import { LocalLocks, ReaderSync, type ChannelLike, type ReaderSyncOptions, type RequestFn } from "./runtime.js";
import { MemorySyncStore } from "./store.js";
import { initialState, SyncError, type Context, type Revision } from "./types.js";
import { decodeRangeWrite, decodeWrite, encodeChange, encodeEntry, encodePosition, encodeRangeResult, encodeResult, encodeState } from "./wire.js";

const T = Date.parse("2026-11-02T12:00:00Z");
const CTX: Context = { actor: 1n, token: "human", receipt: T };

class HttpError extends Error {
  constructor(readonly status: number) {
    super(`http ${status}`);
  }
}

const bodyOf = (options?: RequestInit): JsonValue => {
  const raw = options?.body;
  if (typeof raw !== "string") throw new Error("missing request body");
  return parseJson(raw);
};

const decodeCursor = (text: string): Revision => {
  const i = text.indexOf(":");
  return { generation: text.slice(0, i), rev: BigInt(text.slice(i + 1)) };
};

// server answers request() calls with the same JSON the Go API emits.
// cursorGone/generation failures map to HTTP 410, matching the service.
function server(model: Model): RequestFn {
  const fail = (err: unknown): never => {
    if (err instanceof SyncError && (err.code === "generation" || err.code === "cursor_gone")) throw new HttpError(410);
    throw err;
  };
  return <T>(path: string, options?: RequestInit): Promise<T> => {
    const u = new URL(path, "https://test");
    const q = (k: string) => u.searchParams.get(k) ?? "";
    let body: JsonValue;
    if (u.pathname === "/v1/bootstrap") {
      body = { generation: model.getGeneration(), rev: model.cursor().rev };
    } else if (u.pathname.startsWith("/v1/bootstrap/")) {
      const kind = u.pathname.slice("/v1/bootstrap/".length);
      const res = model.resources();
      const items: JsonValue[] =
        kind === "entries"
          ? res.entries.map(encodeEntry)
          : kind === "state"
            ? res.states.map(encodeState)
            : kind === "positions"
              ? res.positions.map(encodePosition)
              : kind === "subscriptions"
                ? res.objects.filter((o) => o.kind === "subscription").map((o) => ({ id: o.id, folder_id: o.folderId, title: o.name, rev: o.rev, deleted: o.deleted }))
                : kind === "folders"
                  ? res.objects.filter((o) => o.kind === "folder").map((o) => ({ id: o.id, name: o.name, rev: o.rev, deleted: o.deleted }))
                  : kind === "labels"
                    ? []
                    : res.objects.filter((o) => o.kind === "label").map((o) => ({ id: o.id, kind: "label", label_id: o.id, entry_id: o.entryId, name: o.name, principal: o.principal, rev: o.rev, deleted: o.deleted }));
      body = { items, next_id: 0n, next_principal: 0n, next_view: "", has_more: false };
    } else if (u.pathname === "/v1/changes") {
      const cursor = decodeCursor(q("cursor"));
      try {
        const page = model.changes(cursor, Number(q("limit") || "500"));
        body = {
          changes: page.changes.map((c) => {
            const enc = asObj(encodeChange(c), "change");
            const { rev, type, ...payload } = enc;
            return { rev, type, payload };
          }),
          next: { generation: model.getGeneration(), rev: page.next.rev },
          next_cursor: `${model.getGeneration()}:${page.next.rev}`,
        };
      } catch (err) {
        return fail(err);
      }
    } else if (u.pathname === "/v1/state:batch") {
      const writes = asArray(bodyOf(options), "batch").map(decodeWrite);
      try {
        body = model.batch(CTX, writes).map(encodeResult);
      } catch (err) {
        return fail(err);
      }
    } else if (u.pathname === "/v1/state:through") {
      const req = decodeRangeWrite(bodyOf(options));
      try {
        body = encodeRangeResult(model.through(CTX, req));
      } catch (err) {
        return fail(err);
      }
    } else {
      throw new Error(`unexpected path ${path}`);
    }
    return Promise.resolve(stringifyJson(body) as T);
  };
}

const entry = (id: bigint, seq = id) => ({ id, subscriptionId: 1n, seq, firstSeenAt: T, publishedAt: T, rev: 0n, deleted: false });

interface Env {
  model: Model;
  store: MemorySyncStore;
  locks: LocalLocks;
  channel: FakeChannel;
  request: RequestFn;
}

// FakeChannel is a BroadcastChannel stand-in shared by the runtimes in a
// test, delivering messages synchronously to peer listeners.
class FakeChannel implements ChannelLike {
  peers: FakeChannel[] = [];
  listeners: ((m: string) => void)[] = [];
  postMessage(message: string): void {
    for (const p of this.peers) if (p !== this) for (const l of p.listeners) l(message);
  }
  onMessage(fn: (m: string) => void): void {
    this.listeners.push(fn);
  }
  close(): void {
    this.listeners = [];
    this.peers = this.peers.filter((p) => p !== this);
  }
}

let counter = 0;

const env = (): Env => {
  const model = new Model("g1");
  const store = new MemorySyncStore();
  const locks = new LocalLocks();
  const channel = new FakeChannel();
  return { model, store, locks, channel, request: server(model) };
};

const sync = (e: Env, extra: Partial<ReaderSyncOptions> = {}) =>
  new ReaderSync({ request: e.request, principal: 1n, store: e.store, locks: e.locks, channel: e.channel, openEvents: () => null, now: () => T, newOpId: () => `op-${counter++}`, ...extra });

const drive = async (rs: ReaderSync, steps = 8): Promise<void> => {
  for (let i = 0; i < steps; i++) await rs.stepOnce();
};

describe("ReaderSync", () => {
  it("bootstraps all kinds, pulls changes, queues and flushes intent", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    e.model.putObject({ kind: "subscription", id: 1n, principal: 0n, entryId: 0n, folderId: 9n, name: "s", rev: 0n, deleted: false });
    const rs = sync(e);
    await rs.start();
    await drive(rs);
    expect(rs.status().bootstrapped).toBe(true);
    expect(rs.cursor().rev).toBe(2n);
    expect(rs.resource("subscription", 1n)).toBeDefined();
    const w = await rs.queue(1n, "seen", "seen");
    expect(w.base.generation).toBe("g1");
    expect(rs.state(1n, 1n).seen.value).toBe("seen");
    await drive(rs);
    expect(rs.status().outboxSize).toBe(0);
    expect(e.model.state(1n, 1n).seen.value).toBe("seen");
    await rs.stop();
  });

  it("distinguishes synced tombstones from entries outside the cache", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const rs = sync(e);
    await rs.start();
    await drive(rs);
    expect(rs.isDeleted(1n)).toBe(false);
    expect(rs.isDeleted(99n)).toBe(false);
    e.model.deleteEntry(1n);
    await drive(rs);
    expect(rs.hasEntry(1n)).toBe(true);
    expect(rs.isDeleted(1n)).toBe(true);
    await rs.stop();
  });

  it("re-bootstraps on 410 and replays the outbox", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const rs = sync(e);
    await rs.start();
    await drive(rs);
    await rs.queue(1n, "seen", "seen");
    // A server-side change the client never saw, then its cursor expires.
    e.model.batch(CTX, [{ opId: "other", principal: 1n, entryId: 1n, register: "shelf", value: "later", base: { generation: "g1", rev: 0n }, occurredAt: T, kind: "explicit" }]);
    e.model.retainAfter(2n);
    await rs.stepOnce();
    await drive(rs);
    expect(rs.status().outboxSize).toBe(0);
    expect(rs.state(1n, 1n).seen.value).toBe("seen");
    expect(rs.state(1n, 1n).shelf.value).toBe("later");
    await rs.stop();
  });

  it("retries the exact persisted in-flight payload after an error", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const sent: string[] = [];
    const flaky: RequestFn = <T,>(path: string, options?: RequestInit): Promise<T> => {
      if (path === "/v1/state:batch") {
        sent.push(String(options?.body));
        if (sent.length === 1) return Promise.reject(new HttpError(503));
      }
      return e.request(path, options);
    };
    const rs = sync(e, { request: flaky });
    await rs.start();
    await drive(rs);
    await rs.queue(1n, "seen", "seen");
    await expect(rs.stepOnce()).rejects.toThrow("503");
    // A newer same-register intent while the first is in flight must not
    // rewrite the frozen payload.
    await rs.queue(1n, "seen", "unseen");
    await rs.stepOnce();
    expect(sent[0]).toBe(sent[1]);
    expect((await e.store.load())?.pending.length).toBeGreaterThan(0);
    await drive(rs);
    expect(rs.status().outboxSize).toBe(0);
    await rs.stop();
  });

  it("resumes a persisted snapshot: queued intent flushes after restart", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const first = sync(e);
    await first.start();
    await drive(first);
    await first.queue(1n, "seen", "seen");
    await first.stop();
    const second = sync(e);
    await second.start();
    await drive(second);
    expect(second.status().outboxSize).toBe(0);
    expect(e.model.state(1n, 1n).seen.value).toBe("seen");
    await second.stop();
  });

  it("elects one leader per device; a follower's queued intent flushes through the shared snapshot", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const ch1 = new FakeChannel();
    const ch2 = new FakeChannel();
    ch1.peers = [ch1, ch2];
    ch2.peers = ch1.peers;
    const leader = new ReaderSync({ request: e.request, principal: 1n, store: e.store, locks: e.locks, channel: ch1, openEvents: () => null, now: () => T, newOpId: () => `op-${counter++}` });
    const follower = new ReaderSync({ request: e.request, principal: 1n, store: e.store, locks: e.locks, channel: ch2, openEvents: () => null, now: () => T, newOpId: () => `op-${counter++}` });
    await leader.start();
    await follower.start();
    await drive(leader);
    await drive(follower);
    expect(leader.status().leader).toBe(true);
    expect(follower.status().leader).toBe(false);
    await follower.queue(1n, "seen", "seen");
    await leader.stepOnce();
    expect(leader.status().outboxSize).toBe(0);
    expect(e.model.state(1n, 1n).seen.value).toBe("seen");
    await follower.stepOnce();
    expect(follower.state(1n, 1n).seen.value).toBe("seen");
    await leader.stop();
    await follower.stop();
  });

  it("ignores a flush response delivered under a superseded generation", async () => {
    const e = env();
    e.model.ingest(entry(1n), T);
    const inflight: { resolve?: (v: string) => void; opId?: string } = {};
    const hold: RequestFn = <T,>(path: string, options?: RequestInit): Promise<T> => {
      if (path === "/v1/state:batch") {
        inflight.opId = String(asObj(asArray(bodyOf(options), "batch")[0], "write").op_id);
        return new Promise<T>((r) => {
          inflight.resolve = r as (v: string) => void;
        });
      }
      return e.request(path, options);
    };
    const rs1 = sync(e, { request: hold });
    await rs1.start();
    await drive(rs1);
    await rs1.queue(1n, "seen", "seen");
    const flush = rs1.stepOnce();
    for (let i = 0; i < 100 && !inflight.resolve; i++) await Promise.resolve();
    if (!inflight.resolve || !inflight.opId) throw new Error("flush not in flight");
    // A peer rotates the shared snapshot to generation g2 before the held
    // response resolves. Its own writes 410, so it re-bootstraps under g2
    // without consuming the pending op.
    const g2 = Model.restore(e.model, "g2");
    const g2gets = server(g2);
    const peerReq: RequestFn = <T,>(path: string, options?: RequestInit): Promise<T> =>
      path.startsWith("/v1/state:") ? Promise.reject(new HttpError(410)) : g2gets(path, options);
    const peer = sync(e, { request: peerReq });
    await peer.start();
    await drive(peer);
    expect(peer.cursor().generation).toBe("g2");
    inflight.resolve(stringifyJson([encodeResult({ opId: inflight.opId as string, status: "applied", state: initialState(1n, 1n), replay: false })]));
    await flush;
    // The g1 acknowledgement must not clear the g2-rebased intent.
    expect(rs1.status().outboxSize).toBe(1);
    await rs1.stop();
    await peer.stop();
  });
});
