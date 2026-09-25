// The scoped-token "sees zero repositories" checks read the repository list out
// of the tools/call truth envelope. These tests pin the envelope shape the MCP
// server really returns, so the reader cannot go back to looking at the top
// level (where the list never is) and reporting zero rows for a leak.
import { describe, expect, it } from "vitest";

import { assertEmptyGrantRepositoryList, repositoryListFromEnvelope } from "./authMcpE2ERepositoryRows.ts";

// leakedEnvelope is the shape structuredContent has on the wire: the payload
// under data, truth and error beside it.
const leakedEnvelope = {
  data: { repositories: [{ id: "e2e-seed-repo-default", name: "e2e-seed-repo-default" }], total: 1 },
  truth: { level: "exact", profile: "local_authoritative" },
  error: null,
};

// legacyRead is the pre-fix reader: the payload at the top level of the value.
const legacyRead = (structured: unknown): number =>
  ((structured as { repositories?: readonly unknown[] }).repositories ?? []).length;

describe("repositoryListFromEnvelope", () => {
  it("shows the old top-level read was vacuous: an envelope with a leaked repo reads as 0 rows", () => {
    expect(legacyRead(leakedEnvelope)).toBe(0);
  });

  it("reads the leaked repository out of data, so a zero-rows assertion throws on a leak", () => {
    const list = repositoryListFromEnvelope(leakedEnvelope);
    expect(list.rows.map((r) => r.id)).toEqual(["e2e-seed-repo-default"]);
    expect(list.total).toBe(1);
    expect(() => {
      if (list.rows.length !== 0) throw new Error("scope-escalation");
    }).toThrow(/scope-escalation/);
  });

  it("reads an empty grant as zero rows", () => {
    const list = repositoryListFromEnvelope({ data: { repositories: [], total: 0 }, truth: {}, error: null });
    expect(list.rows).toEqual([]);
  });

  it("fails closed on a shape it does not recognise instead of reporting zero rows", () => {
    expect(() => repositoryListFromEnvelope({ repositories: [{ id: "x" }] })).toThrow(/no envelope data/);
    expect(() => repositoryListFromEnvelope({ data: {}, error: null })).toThrow(/no repositories array/);
    expect(() => repositoryListFromEnvelope({ data: { repositories: "nope" } })).toThrow(/no repositories array/);
    expect(() => repositoryListFromEnvelope(null)).toThrow(/not an object/);
    expect(() => repositoryListFromEnvelope("text")).toThrow(/not an object/);
    expect(() => repositoryListFromEnvelope({ data: null, error: { code: "permission_denied" } })).toThrow(/carries an error/);
  });
});

describe("assertEmptyGrantRepositoryList", () => {
  it("throws when the envelope carries a leaked repository", () => {
    expect(() => assertEmptyGrantRepositoryList(leakedEnvelope, "the scoped token")).toThrow(/scope-escalation.*e2e-seed-repo-default/);
  });
  it("passes an envelope whose grant saw nothing, and reports the total", () => {
    expect(assertEmptyGrantRepositoryList({ data: { repositories: [], total: 0 }, truth: {}, error: null }, "the scoped token")).toContain("0 repositories");
  });
  it("throws on an unrecognised shape rather than passing", () => {
    expect(() => assertEmptyGrantRepositoryList({ repositories: [] }, "the scoped token")).toThrow(/no envelope data/);
  });
});
