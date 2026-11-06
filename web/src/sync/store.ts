// Snapshot persistence for the sync engine. IndexedDB stores the whole
// client snapshot (cache, cursor and outbox, including in-flight markers) as
// one structured-clone record per write, so a save is one transaction and a
// crash never tears a page. bigint survives structured clone, keeping 64-bit
// identities exact.

import type { ClientSnapshot } from "./client.js";

export interface SyncStore {
  load(): Promise<ClientSnapshot | null>;
  save(snapshot: ClientSnapshot): Promise<void>;
  // loadVersion returns the version of the persisted snapshot, or null when
  // the store has no version record (including a pre-version snapshot). The
  // version travels inside the snapshot and also in a small record so a
  // reader can check freshness cheaply.
  loadVersion(): Promise<number | null>;
}

const deep = <T>(v: T): T => (typeof structuredClone === "function" ? structuredClone(v) : v);

const versionKey = (device: string) => `${device}:version`;

export class MemorySyncStore implements SyncStore {
  private snapshot: ClientSnapshot | null = null;
  private version: number | null = null;
  load(): Promise<ClientSnapshot | null> {
    return Promise.resolve(this.snapshot === null ? null : deep(this.snapshot));
  }
  save(snapshot: ClientSnapshot): Promise<void> {
    this.snapshot = deep(snapshot);
    this.version = snapshot.version ?? null;
    return Promise.resolve();
  }
  loadVersion(): Promise<number | null> {
    return Promise.resolve(this.version);
  }
}

interface IdbLike {
  open(name: string, version: number, upgrade: (db: IDBDatabase) => void): Promise<IDBDatabase>;
}

const idbOpen: IdbLike["open"] = (name, version, upgrade) =>
  new Promise((resolve, reject) => {
    const req = indexedDB.open(name, version);
    req.onupgradeneeded = () => upgrade(req.result);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("indexedDB open failed"));
  });

function idbRequest<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error("indexedDB request failed"));
  });
}

export class IdbSyncStore implements SyncStore {
  private db: Promise<IDBDatabase> | null = null;
  constructor(
    private readonly dbName: string,
    private readonly device: string,
  ) {}

  private open(): Promise<IDBDatabase> {
    this.db ??= idbOpen(this.dbName, 1, (db) => {
      db.createObjectStore("sync");
    });
    return this.db;
  }

  async load(): Promise<ClientSnapshot | null> {
    const db = await this.open();
    const tx = db.transaction("sync", "readonly");
    const got = await idbRequest(tx.objectStore("sync").get(this.device));
    return got === undefined ? null : (got as ClientSnapshot);
  }

  async loadVersion(): Promise<number | null> {
    const db = await this.open();
    const tx = db.transaction("sync", "readonly");
    const got = await idbRequest(tx.objectStore("sync").get(versionKey(this.device)));
    return got === undefined ? null : Number(got);
  }

  async save(snapshot: ClientSnapshot): Promise<void> {
    const db = await this.open();
    const tx = db.transaction("sync", "readwrite");
    const store = tx.objectStore("sync");
    // The snapshot and its version commit together, so a reader never sees a
    // version without the matching document.
    store.put(snapshot, this.device);
    store.put(snapshot.version ?? 0, versionKey(this.device));
    await new Promise<void>((resolve, reject) => {
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error ?? new Error("IndexedDB transaction failed"));
      tx.onabort = () => reject(tx.error ?? new Error("IndexedDB transaction aborted"));
    });
  }
}

export function defaultStore(dbName: string, device: string): SyncStore {
  if (typeof indexedDB !== "undefined") return new IdbSyncStore(dbName, device);
  return new MemorySyncStore();
}
