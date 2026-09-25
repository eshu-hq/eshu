// Unit tests for the catalog-sweep verdict logic (issue #5167). The sweep is a
// proof gate, so these pin the verdicts a live stack cannot conveniently
// produce on demand: a silent 403 on a ledger route, an unmounted route, an
// unexpected 403 on an allowlisted route, and a tool that slipped past the
// argument table.
import { describe, expect, it } from "vitest";

import type { McpJsonRpcResult } from "./authMcpE2EJsonRpc.ts";
import {
  ROUTE_DENIED,
  classifyToolCallOutcome,
  coverageProblems,
  judgeRow,
  renderSweepTable,
  routeDeniedMessage,
  substituteSeedIds,
  type SweepPolicyRow,
} from "./authMcpE2ECatalogSweepClassify.ts";

const rpc = (result: unknown, httpStatus = 200): McpJsonRpcResult => ({
  httpStatus,
  wwwAuthenticate: null,
  bodyText: JSON.stringify({ result }),
  json: { result } as Record<string, unknown>,
});

const okResult = rpc({ isError: false, content: [{ type: "text", text: "fine" }], structuredContent: { data: {}, truth: {}, error: null } });
const routeDenied = rpc({
  isError: true,
  content: [{ type: "text", text: "denied" }],
  structuredContent: { data: null, truth: null, error: { code: "permission_denied", message: routeDeniedMessage } },
});
const disclosurePattern = /\b403\b|shared[- ]key|refused/i;
const allow: SweepPolicyRow = {
  tool: "get_repo_summary", label: "default", method: "GET", path: "/api/v0/repositories/x/summary",
  class: "allowlisted", arguments: {}, accept: ["ok"],
};
const ledger: SweepPolicyRow = { ...allow, tool: "execute_cypher_query", path: "/api/v0/code/cypher", class: "shared_key_only", accept: [] };

describe("classifyToolCallOutcome", () => {
  it("names a successful envelope ok", () => {
    expect(classifyToolCallOutcome(okResult).outcome).toBe("ok");
  });
  it("separates the route-policy 403 from other permission_denied answers", () => {
    expect(classifyToolCallOutcome(routeDenied).outcome).toBe(ROUTE_DENIED);
    const other = rpc({ isError: true, structuredContent: { error: { code: "permission_denied", message: "role lacks feature" } } });
    expect(classifyToolCallOutcome(other).outcome).toBe("permission_denied");
  });
  it("recognises the non-envelope 403 and unmounted 404 shapes", () => {
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: `HTTP 403: ${routeDeniedMessage}` }] })).outcome).toBe(ROUTE_DENIED);
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: "HTTP 404: 404 page not found" }] })).outcome).toBe("route_unmounted");
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: "HTTP 500: boom" }] })).outcome).toBe("http_500");
  });
  it("never reads an unrecognised body as success", () => {
    expect(classifyToolCallOutcome({ httpStatus: 200, wwwAuthenticate: null, bodyText: "<html>", json: null }).outcome).toBe("unrecognized_response");
    expect(classifyToolCallOutcome(rpc(undefined)).outcome).toBe("unrecognized_response");
    expect(classifyToolCallOutcome({ ...okResult, httpStatus: 401 }).outcome).toBe("http_401");
  });
});

describe("judgeRow", () => {
  it("passes an allowlisted tool only on an accepted outcome", () => {
    expect(judgeRow(allow, { outcome: "ok", detail: "" }, "", disclosurePattern).pass).toBe(true);
    expect(judgeRow(allow, { outcome: ROUTE_DENIED, detail: "" }, "", disclosurePattern).pass).toBe(false);
    expect(judgeRow(allow, { outcome: "route_unmounted", detail: "" }, "", disclosurePattern).pass).toBe(false);
    expect(judgeRow(allow, { outcome: "http_500", detail: "" }, "", disclosurePattern).pass).toBe(false);
    expect(judgeRow({ ...allow, accept: ["ok", "not_found"] }, { outcome: "not_found", detail: "" }, "", disclosurePattern).pass).toBe(true);
  });
  it("passes a ledger tool only on a disclosed route-policy 403", () => {
    const denied = { outcome: ROUTE_DENIED, detail: "" };
    expect(judgeRow(ledger, denied, "Shared-key callers only.", disclosurePattern).pass).toBe(true);
    expect(judgeRow(ledger, denied, "Runs a Cypher query.", disclosurePattern).pass).toBe(false);
    expect(judgeRow(ledger, { outcome: "ok", detail: "" }, "Shared-key callers only.", disclosurePattern).pass).toBe(false);
  });
});

describe("coverageProblems", () => {
  it("fails a listed tool with no argument-table entry, and a stale entry", () => {
    expect(coverageProblems(["get_repo_summary"], [allow])).toEqual([]);
    expect(coverageProblems(["get_repo_summary", "brand_new_tool"], [allow])[0]).toContain("brand_new_tool");
    expect(coverageProblems([], [allow])[0]).toContain("does not list it");
  });
});

describe("substituteSeedIds and renderSweepTable", () => {
  it("substitutes both placeholders recursively", () => {
    expect(substituteSeedIds({ a: "$REPO", b: ["x-$OTHER_REPO"], c: 3 }, { granted: "g", ungranted: "u" })).toEqual({ a: "g", b: ["x-u"], c: 3 });
  });
  it("prints the pass count", () => {
    const rows = [judgeRow(allow, { outcome: "ok", detail: "" }, "", disclosurePattern), judgeRow(ledger, { outcome: "ok", detail: "" }, "", disclosurePattern)];
    expect(renderSweepTable(rows)).toContain("1/2 calls passed");
  });
});
