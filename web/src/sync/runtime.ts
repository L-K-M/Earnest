// ReaderSync is the browser orchestrator over the pure SyncClient engine.
// It owns the network schedule (bootstrap, change pages, outbox flush),
// IndexedDB persistence, SSE change hints and cross-tab coordination. Every
// mutation runs inside a Web Lock against the shared persisted snapshot, so
// two tabs cannot interleave a read-modify-write on the outbox or cursor.
//
// Network contract: `request` resolves a path to its decoded JSON body (or
// raw text, which this layer decodes losslessly) and throws an error
// carrying a numeric `status` on HTTP failures. The parent wires it to the
// shared fetch helper (`web/src/request.ts`), which also attaches CSRF.

import { SyncClient, type ClientSnapshot, type DroppedOperation, type FlushRequest } from "./client.js";
import { asArray, asObj, boolIn, intIn, parseJson, strIn, stringifyJson, type JsonObject, type JsonValue } from "./json.js";
import { MemorySyncStore, defaultStore, type SyncStore } from "./store.js";
import {
  errInvalid,
  SyncError,
  type OperationKind,
  type Entry,
  type RangeWrite,
  type RegisterName,
  type Result,
  type Revision,
  type Scope,
  type State,
  type SyncObject,
  type Value,
  type Write,
} from "./types.js";
import {
  decodeChangeRecord,
  decodeEntry,
  decodeObject,
  decodePosition,
  decodeRangeResult,
  decodeResult,
  decodeState,
  encodeScope,
  encodeWrite,
} from "./wire.js";

export type RequestFn = <T>(path: string, options?: RequestInit) => Promise<T>;

export interface HttpStatusError {
  status?: number;
}

export const httpStatus = (err: unknown): number | undefined =>
  typeof err === "object" && err !== null ? (err as HttpStatusError).status : undefined;

// LockManager abstracts navigator.locks so tests and unsupported browsers
// share one implementation path.
export interface LockManager {
  request<T>(name: string, fn: () => T | Promise<T>): Promise<T>;
  tryHold(name: string): Promise<(() => void) | null>;
}

export class WebLocks implements LockManager {
  request<T>(name: string, fn: () => T | Promise<T>): Promise<T> {
    return navigator.locks.request(name, fn) as Promise<T>;
  }
  tryHold(name: string): Promise<(() => void) | null> {
    return new Promise((resolve) => {
      let settled = false;
      void navigator.locks
        .request(name, { ifAvailable: true }, (lock) => {
          if (lock === null) {
            if (!settled) {
              settled = true;
              resolve(null);
            }
            return;
          }
          return new Promise<void>((done) => {
            if (!settled) {
              settled = true;
              resolve(() => done());
            }
          });
        })
        .catch(() => {
          if (!settled) {
            settled = true;
            resolve(null);
          }
        });
    });
  }
}

// LocalLocks is the single-tab fallback and the test double: an in-memory
// mutex for request and an ownership set for tryHold. Two ReaderSyncs sharing
// one LocalLocks and one store simulate two tabs.
export class LocalLocks implements LockManager {
  private chains = new Map<string, Promise<unknown>>();
  private held = new Set<string>();
  request<T>(name: string, fn: () => T | Promise<T>): Promise<T> {
    const prev = this.chains.get(name) ?? Promise.resolve();
    const next = prev.then(fn, fn);
    this.chains.set(
      name,
      next.catch(() => undefined),
    );
    return next;
  }
  tryHold(name: string): Promise<(() => void) | null> {
    if (this.held.has(name)) return Promise.resolve(null);
    this.held.add(name);
    return Promise.resolve(() => {
      this.held.delete(name);
    });
  }
}

export interface EventHandle {
  close(): void;
}

export interface ChannelLike {
  postMessage(message: string): void;
  onMessage(fn: (message: string) => void): void;
  close(): void;
}

class BrowserChannel implements ChannelLike {
  private ch: BroadcastChannel;
  constructor(name: string) {
    this.ch = new BroadcastChannel(name);
  }
  postMessage(message: string): void {
    this.ch.postMessage(message);
  }
  onMessage(fn: (message: string) => void): void {
    this.ch.onmessage = (e) => fn(String(e.data));
  }
  close(): void {
    this.ch.close();
  }
}

export interface ReaderSyncOptions {
  request: RequestFn;
  principal: bigint;
  device?: string;
  store?: SyncStore;
  locks?: LockManager;
  channel?: ChannelLike | null;
  // openEvents subscribes to the hint channel; invoking onHint must trigger
  // a pull (SSE carries cursors, never payloads).
  openEvents?: (onHint: () => void, signal: AbortSignal) => EventHandle | null;
  now?: () => number;
  newOpId?: () => string;
  pageLimit?: number;
  bootstrapLimit?: number;
  onDropped?: (dropped: DroppedOperation[]) => void;
  onError?: (err: unknown) => void;
}

export interface SyncStatus {
  cursor: Revision;
  outboxSize: number;
  inflight: boolean;
  syncing: boolean;
  leader: boolean;
  bootstrapped: boolean;
  lastError: string | null;
}

const BOOTSTRAP_KINDS = ["subscriptions", "folders", "labels", "label_memberships", "positions", "state", "entries"] as const;
type BootstrapKind = (typeof BOOTSTRAP_KINDS)[number];

// bootstrapFlushPages bounds how many bootstrap pages accumulate before one
// reload/apply/persist cycle. It trades a little memory for far fewer full
// snapshot reads and writes on a large account.
const bootstrapFlushPages = 8;

// request resolves bodies through ordinary JSON.parse, where every integer is
// a number. Safe integers become bigint so the strict wire decoders see the
// same tree shape as the lossless parser produces. The shared helper already
// refuses unsafe integers, so a surviving number is always exact.
const normalizeNumbers = (v: unknown): JsonValue => {
  if (typeof v === "number") return Number.isSafeInteger(v) ? BigInt(v) : v;
  if (Array.isArray(v)) return v.map(normalizeNumbers);
  if (v !== null && typeof v === "object") {
    const out: JsonObject = {};
    for (const [k, x] of Object.entries(v)) out[k] = normalizeNumbers(x);
    return out;
  }
  if (v === undefined) return null;
  return v as JsonValue;
};

const asBody = async (request: RequestFn, path: string, options?: RequestInit): Promise<JsonValue> => {
  const raw = await request<unknown>(path, options);
  if (typeof raw === "string") return parseJson(raw);
  if (raw === null || raw === undefined) return {};
  return normalizeNumbers(raw);
};

const normalizeValue = (register: RegisterName, value: Value | boolean): Value => {
  if (typeof value === "boolean") return value ? "true" : "false";
  return value;
};

export class ReaderSync {
  private readonly request: RequestFn;
  private readonly principal: bigint;
  private readonly device: string;
  private readonly store: SyncStore;
  private readonly locks: LockManager;
  private readonly channel: ChannelLike | null;
  private readonly openEvents: ((onHint: () => void, signal: AbortSignal) => EventHandle | null) | null;
  private readonly now: () => number;
  private readonly newOpId: () => string;
  private readonly pageLimit: number;
  private readonly bootstrapLimit: number;
  private readonly onDropped: ((dropped: DroppedOperation[]) => void) | null;
  private readonly onError: ((err: unknown) => void) | null;

  private client: SyncClient;
  private rawItems = new Map<string, JsonValue>();
  private listeners = new Set<(status: SyncStatus) => void>();
  private running = false;
  private leader = false;
  private releaseLead: (() => void) | null = null;
  private busy: Promise<void> | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private events: EventHandle | null = null;
  private abort = new AbortController();
  private failures = 0;
  private lastError: string | null = null;
  private bootstrapped = false;
  // version is the store generation of the in-memory snapshot. It lets a
  // mutate() skip the full reload when no other tab has written since the
  // last load, which is the common case for a single reader. It starts below
  // zero so the first comparison always performs a full load.
  private version = -1;

  constructor(options: ReaderSyncOptions) {
    if (options.principal <= 0n) throw errInvalid("principal required");
    this.request = <T>(path: string, init?: RequestInit) => options.request<T>(path, {
      ...init,
      signal: init?.signal ? AbortSignal.any([init.signal, this.abort.signal]) : this.abort.signal,
    });
    this.principal = options.principal;
    this.device = options.device ?? `principal:${this.principal}`;
    this.store = options.store ?? defaultStore("earnest-sync", this.device);
    this.locks = options.locks ?? (typeof navigator !== "undefined" && navigator.locks ? new WebLocks() : new LocalLocks());
    this.channel = options.channel === undefined ? (typeof BroadcastChannel !== "undefined" ? new BrowserChannel(`earnest-sync:${this.device}`) : null) : options.channel;
    this.openEvents =
      options.openEvents === undefined
        ? typeof EventSource !== "undefined"
          ? (onHint: () => void, signal: AbortSignal) => this.eventSource(onHint, signal)
          : null
        : options.openEvents;
    this.now = options.now ?? Date.now;
    this.newOpId = options.newOpId ?? (() => crypto.randomUUID());
    this.pageLimit = options.pageLimit ?? 500;
    this.bootstrapLimit = options.bootstrapLimit ?? 1000;
    this.onDropped = options.onDropped ?? null;
    this.onError = options.onError ?? null;
    this.client = new SyncClient({ generation: "", rev: 0n });
  }

  private eventSource(onHint: () => void, signal: AbortSignal): EventHandle | null {
    const source = new EventSource("/v1/events", { withCredentials: true });
    source.addEventListener("changes", () => onHint());
    signal.addEventListener("abort", () => source.close(), { once: true });
    return source;
  }

  // start loads the persisted snapshot (or bootstraps a fresh device),
  // reports locally dead intent, then joins the poll loop.
  async start(): Promise<void> {
    if (this.running) return;
    this.running = true;
    await this.mutate(() => {
      const dropped = this.client.expireQueue(this.now());
      if (dropped.length > 0) this.reportDropped(dropped);
    });
    this.channel?.onMessage((message) => {
      if (message === "changed") {
        void this.refresh();
        return;
      }
      this.kick();
    });
    await this.acquireLead();
    this.events = this.openEvents === null ? null : this.openEvents(() => this.kick(), this.abort.signal);
    this.kick();
  }

  async stop(): Promise<void> {
    this.running = false;
    this.abort.abort();
    this.events?.close();
    this.events = null;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    this.channel?.close();
    if (this.busy !== null) await this.busy.catch(() => undefined);
    this.releaseLead?.();
    this.releaseLead = null;
    this.leader = false;
  }

  // queue records local intent. The write's base is the last server revision
  // the cache observed for that register; the outbox coalesces the rest.
  async queue(entryId: bigint, register: RegisterName, value: Value | boolean, kind: OperationKind = "explicit"): Promise<Write> {
    const v = normalizeValue(register, value);
    return this.mutate(() => {
      const w: Write = {
        opId: this.newOpId(),
        principal: this.principal,
        entryId,
        register,
        value: v,
        base: this.client.registerBase(this.principal, entryId, register),
        occurredAt: this.now(),
        kind,
      };
      this.client.queue(w);
      return w;
    });
  }

  // queueRange records a mark-all intent with a fixed boundary: an explicit
  // throughSeq, the highest cached seq first seen by `at`, or the highest
  // cached seq when both are omitted.
  async queueRange(scope: Scope, boundary?: { throughSeq?: bigint; at?: number }, kind: OperationKind = "explicit"): Promise<RangeWrite> {
    return this.mutate(() => {
      let through = boundary?.throughSeq;
      if (through === undefined) {
        through = 0n;
        for (const e of this.client.entriesAll()) {
          if (e.deleted) continue;
          if (boundary?.at !== undefined && e.firstSeenAt > boundary.at) continue;
          if (e.seq > through) through = e.seq;
        }
      }
      const r: RangeWrite = {
        write: {
          opId: this.newOpId(),
          principal: this.principal,
          entryId: 1n,
          register: "seen",
          value: "seen",
          base: { generation: this.client.getCursor().generation, rev: 0n },
          occurredAt: this.now(),
          kind,
        },
        scope: { kind: scope.kind, ids: [...scope.ids] },
        throughSeq: through,
      };
      this.client.queueRange(r);
      return r;
    });
  }

  // state is server state plus the pending overlay for one principal/entry.
  state(principal: bigint, entryId: bigint): State {
    return this.client.state(principal, entryId);
  }

  hasEntry(entryId: bigint): boolean {
    return this.client.entry(entryId) !== undefined;
  }

  // History pages can extend beyond bootstrap's cached window. Adopt what
  // this device actually observed before deriving the register base.
  async observe(entry: Entry, state: State, generation?: string): Promise<void> {
    await this.mutate(() => {
      if (generation !== undefined && generation !== this.client.getCursor().generation) throw new SyncError("generation", "Reading state changed while this page was open. Refresh the view.");
      this.client.mergeEntry(entry);
      this.client.mergeState(state);
    });
  }

  cursor(): Revision {
    return this.client.getCursor();
  }

  status(): SyncStatus {
    return {
      cursor: this.client.getCursor(),
      outboxSize: this.client.outbox().length,
      inflight: this.busy !== null,
      syncing: this.busy !== null,
      leader: this.leader,
      bootstrapped: this.bootstrapped,
      lastError: this.lastError,
    };
  }

  // resource exposes the richer bootstrap projections (subscription titles,
  // feed URLs, label definitions) the engine itself does not need.
  resource(kind: string, id: bigint): JsonValue | undefined {
    return this.rawItems.get(`${kind}:${id}`);
  }

  subscribe(listener: (status: SyncStatus) => void): () => void {
    this.listeners.add(listener);
    listener(this.status());
    return () => this.listeners.delete(listener);
  }

  private notify(): void {
    const status = this.status();
    for (const l of this.listeners) l(status);
  }

  private reportDropped(dropped: DroppedOperation[]): void {
    this.onDropped?.(dropped);
  }

  private fail(err: unknown): void {
    this.lastError = err instanceof Error ? err.message : String(err);
    this.onError?.(err);
  }

  private lockName(): string {
    return `earnest-sync:${this.device}`;
  }

  private async withLock<T>(fn: () => T | Promise<T>): Promise<T> {
    return this.locks.request(this.lockName(), fn);
  }

  // mutate serializes reload → apply → persist inside the shared lock, so a
  // second tab's queue() cannot interleave inside one transaction. A failed
  // persist rolls the in-memory view back to the durable snapshot: undurable
  // intent must never be visible as if queued.
  private async mutate<T>(fn: () => T): Promise<T> {
    return this.withLock(async () => {
      await this.reloadIfStale();
      const before = this.client.toSnapshot();
      const wasBootstrapped = this.bootstrapped;
      const rawBefore = new Map(this.rawItems);
      const versionBefore = this.version;
      let out: T;
      try {
        out = fn();
        await this.persist();
      } catch (err) {
        this.client = SyncClient.fromSnapshot(before);
        this.bootstrapped = wasBootstrapped;
        this.rawItems = rawBefore;
        this.version = versionBefore;
        this.notify();
        throw err;
      }
      this.channel?.postMessage("changed");
      this.notify();
      return out;
    });
  }

  // reloadIfStale reads the small version record and only pays for the full
  // snapshot when another tab (or a pre-version store) changed it.
  private async reloadIfStale(): Promise<void> {
    const storedVersion = await this.store.loadVersion();
    // A null version means the store predates versioning and must be read once.
    if (storedVersion !== this.version) await this.reload();
  }

  private async refresh(): Promise<void> {
    await this.withLock(async () => {
      await this.reloadIfStale();
      this.notify();
    });
    this.kick();
  }

  private async reload(): Promise<void> {
    const snap = await this.store.load();
    if (snap !== null) {
      this.client = SyncClient.fromSnapshot(snap);
      // Older caches cannot distinguish gone acknowledgements from tombstones.
      this.bootstrapped = snap.bootstrapped === true && snap.goneEntries !== undefined;
      this.rawItems = new Map(snap.rawItems ?? []);
      this.version = snap.version ?? 0;
    } else {
      this.version = 0;
    }
  }

  private async persist(): Promise<void> {
    const snap = this.client.toSnapshot();
    snap.bootstrapped = this.bootstrapped;
    snap.rawItems = [...this.rawItems];
    this.version += 1;
    snap.version = this.version;
    await this.store.save(snap);
  }

  private async acquireLead(): Promise<void> {
    if (!this.running || this.releaseLead !== null) return;
    const release = await this.locks.tryHold(`${this.lockName()}:poll`);
    if (!this.running) {
      release?.();
      return;
    }
    if (release === null) {
      // Follower: retry leadership shortly; a crashed leader frees the lock.
      this.schedule(1500);
      return;
    }
    this.releaseLead = release;
    this.leader = true;
    this.notify();
    // A pre-leadership kick may still hold busy as a no-op run; wait it out
    // so this kick is not swallowed as a duplicate.
    if (this.busy !== null) await this.busy.catch(() => undefined);
    this.kick();
  }

  private schedule(ms: number): void {
    if (!this.running || this.timer !== null) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.acquireLead().then(() => this.kick()).catch(err => {
        this.fail(err);
        this.schedule(1500);
      });
    }, ms);
  }

  // kick schedules one sync step; concurrent kicks coalesce.
  kick(): void {
    if (!this.running || this.busy !== null) return;
    this.busy = this.run()
      .catch((err) => {
        if (!this.running) return;
        this.failures++;
        this.fail(err);
        this.schedule(Math.min(30000, 250 * 2 ** Math.min(this.failures, 8)));
      })
      .finally(() => {
        this.busy = null;
        this.notify();
      });
  }

  private async run(): Promise<void> {
    if (!this.leader) return;
    let again = false;
    do {
      again = false;
      if (!this.bootstrapped) {
        await this.bootstrap();
        again = true;
      } else if (this.client.outbox().length > 0) {
        again = await this.flushOnce();
      } else {
        again = await this.pullOnce();
      }
    } while (again && this.running);
    if (this.running && !again) this.schedule(this.failures > 0 ? Math.min(30000, 250 * 2 ** this.failures) : 60000);
  }

  private async pullOnce(): Promise<boolean> {
    // A captured cursor without a completed page pass is not a usable
    // cache; a crash mid-bootstrap restarts from the cursor endpoint.
    if (!this.bootstrapped) {
      await this.bootstrap();
      return true;
    }
    const cursor = this.client.getCursor();
    let page;
    try {
      page = await this.getChanges(cursor);
    } catch (err) {
      if (httpStatus(err) === 410) {
        await this.bootstrap();
        return true;
      }
      this.failures++;
      throw err;
    }
    this.failures = 0;
    const { changes, next } = page;
    try {
      await this.mutate(() => this.client.applyPage(changes, next));
    } catch (err) {
      // A gap or a stale generation means the local log can no longer be
      // trusted; only a fresh bootstrap can repair the cache.
      const code = err instanceof SyncError ? err.code : "";
      if (code === "generation" || code === "invalid" || code === "cursor_gone") {
        await this.bootstrap();
        return true;
      }
      throw err;
    }
    return changes.length > 0;
  }

  private async getChanges(cursor: Revision): Promise<{ changes: ReturnType<typeof decodeChangeRecord>[]; next: Revision }> {
    const body = asObj(await asBody(this.request, `/v1/changes?cursor=${encodeURIComponent(`${cursor.generation}:${cursor.rev}`)}&limit=${this.pageLimit}`), "changes");
    const list = body.changes === undefined || body.changes === null ? [] : asArray(body.changes, "changes.changes");
    const nextObj = asObj(body.next ?? {}, "changes.next");
    return {
      changes: list.map(decodeChangeRecord),
      next: { generation: strIn(nextObj, "generation", "changes.next"), rev: intIn(nextObj, "rev", "changes.next") },
    };
  }

  private async bootstrap(): Promise<void> {
    const cursorBody = asObj(await asBody(this.request, "/v1/bootstrap"), "bootstrap");
    const cursor: Revision = {
      generation: strIn(cursorBody, "generation", "bootstrap"),
      rev: intIn(cursorBody, "rev", "bootstrap"),
    };
    // The cursor must be captured before paging; later commits replay
    // through /v1/changes. The flag stays false until every page merged,
    // so an interrupted pass re-bootstraps rather than trusting a
    // partially filled cache.
    await this.mutate(() => {
      this.client.beginBootstrap(cursor);
      this.bootstrapped = false;
      this.rawItems.clear();
    });
    // Pages are buffered so a large bootstrap pays one reload and one persist
    // per batch instead of per page. A crash between batches leaves the
    // snapshot unbootstrapped, so the next run starts over.
    const buffered: {kind: BootstrapKind; items: JsonValue[]}[] = [];
    const flushBuffer = async () => {
      if (buffered.length === 0) return;
      const batch = buffered.splice(0, buffered.length);
      await this.mutate(() => {
        for (const page of batch) {
          for (const item of page.items) this.mergeItem(page.kind, item);
        }
      });
    };
    for (const kind of BOOTSTRAP_KINDS) {
      let afterId = 0n;
      let afterPrincipal = 0n;
      let afterView = "";
      for (;;) {
        const body = asObj(
          await asBody(
            this.request,
            `/v1/bootstrap/${kind}?after_id=${afterId}&after_principal=${afterPrincipal}&after_view=${encodeURIComponent(afterView)}&limit=${this.bootstrapLimit}`,
          ),
          "bootstrap-page",
        );
        const items = body.items === undefined || body.items === null ? [] : asArray(body.items, "bootstrap.items");
        buffered.push({kind, items});
        if (buffered.length >= bootstrapFlushPages) await flushBuffer();
        if (body.has_more !== true) break;
        afterId = intIn(body, "next_id", "bootstrap-page");
        afterPrincipal = intIn(body, "next_principal", "bootstrap-page");
        afterView = strIn(body, "next_view", "bootstrap-page");
      }
    }
    await flushBuffer();
    await this.mutate(() => {
      this.bootstrapped = true;
    });
  }

  private mergeItem(kind: BootstrapKind, item: JsonValue): void {
    switch (kind) {
      case "subscriptions": {
        const o = asObj(item, "subscription");
        const obj: SyncObject = {
          kind: "subscription",
          id: intIn(o, "id", "subscription"),
          principal: 0n,
          entryId: 0n,
          folderId: intIn(o, "folder_id", "subscription"),
          name: strIn(o, "title", "subscription"),
          rev: intIn(o, "rev", "subscription"),
          deleted: boolIn(o, "deleted", "subscription"),
        };
        this.client.mergeObject(obj);
        this.rawItems.set(`subscription:${obj.id}`, item);
        return;
      }
      case "folders": {
        const o = asObj(item, "folder");
        const obj: SyncObject = {
          kind: "folder",
          id: intIn(o, "id", "folder"),
          principal: 0n,
          entryId: 0n,
          folderId: 0n,
          name: strIn(o, "name", "folder"),
          rev: intIn(o, "rev", "folder"),
          deleted: boolIn(o, "deleted", "folder"),
        };
        this.client.mergeObject(obj);
        this.rawItems.set(`folder:${obj.id}`, item);
        return;
      }
      case "labels": {
        // Label definitions are not membership objects; keep the raw
        // projection under its own key so it cannot collide with
        // entry-label membership objects.
        const o = asObj(item, "label");
        const id = intIn(o, "id", "label");
        this.rawItems.set(`labeldef:${id}`, item);
        return;
      }
      case "label_memberships": {
        const obj = decodeObject(item);
        this.client.mergeObject(obj);
        this.rawItems.set(`label:${obj.id}`, item);
        return;
      }
      case "positions": {
        this.client.mergePosition(decodePosition(item));
        return;
      }
      case "state": {
        this.client.mergeState(decodeState(item));
        return;
      }
      case "entries": {
        const entry = decodeEntry(item);
        this.client.mergeEntry(entry);
        this.rawItems.set(`entry:${entry.id}`, item);
        return;
      }
    }
  }

  // flushOnce submits the head of the outbox: consecutive register writes as
  // one atomic batch, a range alone. The in-flight payload is persisted
  // before the request and retried verbatim after transport errors.
  private async flushOnce(): Promise<boolean> {
    const staged = await this.mutate(() => {
      const dropped = this.client.expireQueue(this.now());
      return {request:this.client.beginFlush(), dropped};
    });
    if (staged.dropped.length) this.reportDropped(staged.dropped);
    const req = staged.request;
    if (req === null) return false;
    try {
      if (req.kind === "batch") {
        const body = await asBody(this.request, "/v1/state:batch", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: stringifyJson(req.writes.map(encodeWrite)),
        });
        const results = asArray(body, "state:batch.results").map(decodeResult);
        await this.acknowledge(req, results);
      } else {
        const body = await asBody(this.request, "/v1/state:through", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: stringifyJson({ write: encodeWrite(req.range.write), scope: encodeScope(req.range.scope), through_seq: req.range.throughSeq }),
        });
        const result = decodeRangeResult(body);
        await this.mutate(() => {
          const outcome = this.client.acknowledgeRange(req.requestGeneration, req.range.write.opId, result);
          if (outcome.applied && outcome.dropped.length > 0) this.reportDropped(outcome.dropped);
        });
      }
      this.failures = 0;
      return true;
    } catch (err) {
      if (httpStatus(err) === 410) {
        await this.bootstrap();
        return true;
      }
      this.failures++;
      await this.mutate(() => this.client.flushFailed());
      throw err;
    }
  }

  private async acknowledge(req: FlushRequest, results: Result[]): Promise<void> {
    await this.mutate(() => {
      const outcome = this.client.acknowledge(req.requestGeneration, results);
      if (outcome.applied && outcome.dropped.length > 0) this.reportDropped(outcome.dropped);
    });
  }

  // stepOnce runs exactly one network step (flush or pull); the internal
  // loop uses it and tests drive it directly.
  async stepOnce(): Promise<void> {
    // Another tab may have queued intent since this tab last looked; pick it
    // up before deciding what to do.
    await this.withLock(async () => {
      await this.reloadIfStale();
      this.notify();
    });
    if (this.client.outbox().length > 0) {
      await this.flushOnce();
      return;
    }
    await this.pullOnce();
  }
}

export type { ClientSnapshot, DroppedOperation, SyncStore };
export { MemorySyncStore };
