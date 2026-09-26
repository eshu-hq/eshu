// Unit tests for the catalog-sweep verdict logic (issue #5167). The sweep is a
// proof gate, so these pin the verdicts a live stack cannot conveniently
// produce on demand: a silent 403 on a ledger route, an unmounted route, an
// unexpected 403 on an allowlisted route, and a tool that slipped past the
// argument table.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import type { McpJsonRpcResult } from "./authMcpE2EJsonRpc.ts";
import {
  ASK_DEFAULT_OFF,
  ROUTE_DENIED,
  allScopeControlFor,
  classifyToolCallOutcome,
  compileDisclosurePattern,
  coverageProblems,
  judgeAllScopeControl,
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
    const typed404 = 'HTTP 404: {"detail":"service not found","error":"Not Found"}';
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: typed404 }] })).outcome).toBe("not_found");
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: "HTTP 404: 404 page not found" }] })).outcome).toBe("route_unmounted");
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
    const ids = { granted: "g", ungranted: "u", scope: "s", stateScope: "t" };
    expect(substituteSeedIds({ a: "$REPO", b: ["x-$OTHER_REPO"], c: 3, d: "$SCOPE", e: "$STATE_SCOPE" }, ids)).toEqual({
      a: "g", b: ["x-u"], c: 3, d: "s", e: "t",
    });
  });
  it("prints the pass count", () => {
    const rows = [judgeRow(allow, { outcome: "ok", detail: "" }, "", disclosurePattern), judgeRow(ledger, { outcome: "ok", detail: "" }, "", disclosurePattern)];
    expect(renderSweepTable(rows)).toContain("1/2 calls passed");
  });
});

describe("the disclosure pattern the Go test emits", () => {
  // Read the pattern from the Go source constant, not a hand-written literal:
  // a Go-only construct such as an inline (?i) flag is a SyntaxError in
  // JavaScript, and a literal here would hide exactly that.
  const goSource = readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), "../../../go/internal/mcp/dispatch_catalog_sweep_policy_test.go"),
    "utf8",
  );
  const emitted = /const catalogSweepDisclosurePattern = `([^`]*)`/.exec(goSource)?.[1] ?? "";

  const goFlags = /const catalogSweepDisclosureFlags = "([^"]*)"/.exec(goSource)?.[1] ?? "";

  it("rejects a Go-only inline flag with a message naming the policy", () => {
    expect(() => compileDisclosurePattern({ disclosurePattern: "(?i)403", disclosureFlags: "" })).toThrow(/disclosurePattern/);
  });

  it("is found in the Go source", () => {
    expect(emitted).not.toBe("");
  });

  it("compiles as a JavaScript RegExp exactly as the runner constructs it", () => {
    const policy = { disclosurePattern: emitted, disclosureFlags: goFlags };
    expect(() => compileDisclosurePattern(policy)).not.toThrow();
    const re = compileDisclosurePattern(policy);
    expect(re.test("Refused with a 403 for scoped tokens")).toBe(true);
    expect(re.test("Shared-key callers only")).toBe(true);
    expect(re.test("Runs a Cypher query")).toBe(false);
  });
});

describe("ask default-off classification", () => {
  const off = 'HTTP 503: {"state":"unavailable","reason":"ask is not enabled; set ESHU_ASK_ENABLED=true and configure an agent_reasoning provider profile"}';
  it("names the default-off 503 ask_default_off", () => {
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: off }] })).outcome).toBe(ASK_DEFAULT_OFF);
  });
  it("keeps a genuine backend or engine 503 as http_503 so it fails the sweep", () => {
    const engine = 'HTTP 503: {"state":"unavailable","reason":"ask engine encountered an error; see operator logs"}';
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: engine }] })).outcome).toBe("http_503");
    expect(classifyToolCallOutcome(rpc({ isError: true, content: [{ type: "text", text: "HTTP 503: upstream connect error" }] })).outcome).toBe("http_503");
  });
});

describe("all-scope control for a tolerant row that names the granted repository", () => {
  const ids = { granted: "g-repo", ungranted: "u-repo", scope: "s", stateScope: "t" };
  const tolerant: SweepPolicyRow = {
    tool: "get_file_content", label: "default", method: "POST", path: "/api/v0/content/files/read", class: "allowlisted",
    arguments: { repo_id: "$REPO", relative_path: "README.md" }, accept: ["ok", "not_found"], acceptReason: "relative_path README.md is not seeded",
  };
  it("replays the substituted call for a not_found on the granted repository", () => {
    expect(allScopeControlFor(tolerant, "not_found", ids)).toEqual({
      method: "POST", path: "/api/v0/content/files/read", body: { repo_id: "g-repo", relative_path: "README.md" },
    });
  });
  it("replays the body the MCP dispatcher sends, not the raw tool arguments", () => {
    // analyze_code_relationships renames target -> name on its way to the
    // route, so replaying the raw arguments earns a 400, not the typed 404.
    const renamed: SweepPolicyRow = {
      tool: "analyze_code_relationships", label: "who_modifies", method: "POST", path: "/api/v0/code/relationships", class: "allowlisted",
      arguments: { query_type: "who_modifies", target: "sweepTarget", repo_id: "$REPO", limit: 5 },
      body: { name: "sweepTarget", direction: "incoming", repo_id: "$REPO", limit: 5 },
      accept: ["ok", "not_found"], acceptReason: "target sweepTarget is not seeded",
    };
    expect(allScopeControlFor(renamed, "not_found", ids)).toEqual({
      method: "POST", path: "/api/v0/code/relationships", body: { name: "sweepTarget", direction: "incoming", repo_id: "g-repo", limit: 5 },
    });
  });
  it("replays a GET row's dispatched query string", () => {
    const get: SweepPolicyRow = {
      tool: "get_x", label: "default", method: "GET", path: "/api/v0/x/$REPO", class: "allowlisted",
      arguments: { repo_id: "$REPO", path: "a b" }, query: { path: "a b", repo: "$REPO" },
      accept: ["ok", "not_found"], acceptReason: "path a b is not seeded",
    };
    expect(allScopeControlFor(get, "not_found", ids)).toEqual({
      method: "GET", path: "/api/v0/x/g-repo?path=a+b&repo=g-repo", body: undefined,
    });
  });
  it("needs no control when the row answered ok, or names no granted repository, or is a ledger row", () => {
    expect(allScopeControlFor(tolerant, "ok", ids)).toBeUndefined();
    expect(allScopeControlFor({ ...tolerant, arguments: { entity_id: "sweep-seed-missing" } }, "not_found", ids)).toBeUndefined();
    expect(allScopeControlFor({ ...tolerant, class: "shared_key_only" }, "not_found", ids)).toBeUndefined();
    expect(allScopeControlFor({ ...tolerant, accept: ["ok"] }, "not_found", ids)).toBeUndefined();
  });
  it("passes only when the all-scope session answers the same typed 404", () => {
    expect(judgeAllScopeControl(404, '{"detail":"file not found","error":"Not Found"}').pass).toBe(true);
    expect(judgeAllScopeControl(200, '{"content":"x"}').pass).toBe(false);
    expect(judgeAllScopeControl(404, "404 page not found").pass).toBe(false);
  });
});
