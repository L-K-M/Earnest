// Drift gate: every registered vector in testdata/vectors executes end to
// end. Server steps run on the in-memory Model; every client-side step runs
// through SyncClient. Unregistered files and unknown ops fail hard.

import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { runVector, subsetError, VECTOR_FILES, VectorFailure } from "./harness.js";

const vectorsDir = fileURLToPath(new URL("../../../testdata/vectors/", import.meta.url));
const onDisk = readdirSync(vectorsDir)
  .filter((f) => f.endsWith(".json"))
  .sort();

it("registers every vector file on disk", () => {
  expect(onDisk).toEqual([...VECTOR_FILES].sort());
});

describe.each(VECTOR_FILES)("%s", (file) => {
  it("runs through the TypeScript engine", () => {
    const text = readFileSync(new URL(file, `file://${vectorsDir}/`), "utf8");
    runVector(file, text);
  });
});

const MINIMAL = { version: 1, name: "x", decision: "x", final: [{ op: "assert_log", expect: {} }] };

describe("harness rejection", () => {
  it("rejects an unregistered vector name in the manifest check", () => {
    expect(VECTOR_FILES).not.toContain("99-unknown.json");
  });
  it("rejects an unknown step op", () => {
    const v = { ...MINIMAL, final: [{ op: "unrecognized", expect: {} }] };
    expect(() => runVector("local", JSON.stringify(v))).toThrow(VectorFailure);
  });
  it("rejects an unknown vector field", () => {
    const v = { ...MINIMAL, unexpected: true };
    expect(() => runVector("local", JSON.stringify(v))).toThrow();
  });
  it("rejects an unknown step field", () => {
    const v = { ...MINIMAL, final: [{ op: "assert_log", expect: {}, surprise: 1 }] };
    expect(() => runVector("local", JSON.stringify(v))).toThrow(VectorFailure);
  });
  it("rejects a bad version", () => {
    const v = { ...MINIMAL, version: 99 };
    expect(() => runVector("local", JSON.stringify(v))).toThrow(VectorFailure);
  });
  it("rejects a step without expect", () => {
    const v = { ...MINIMAL, final: [{ op: "assert_log" }] };
    expect(() => runVector("local", JSON.stringify(v))).toThrow(VectorFailure);
  });
  it("reports a wrong error name", () => {
    const v = { ...MINIMAL, final: [{ op: "assert_log", expect: {}, error: "invalid" }] };
    expect(() => runVector("local", JSON.stringify(v))).toThrow(/error ""/);
  });
  it("subset treats expected objects as subsets and arrays as exact", () => {
    expect(subsetError({ a: 1n }, { a: 1n, b: 2n }, "")).toBeNull();
    expect(subsetError({ a: [1n] }, { a: [1n, 2n] }, "")).not.toBeNull();
    expect(subsetError({ a: 1n }, { b: 1n }, "")).not.toBeNull();
    expect(subsetError(9007199254740993n, 9007199254740993n, "")).toBeNull();
    expect(subsetError(9007199254740993n, 9007199254740992n, "")).not.toBeNull();
  });
});
