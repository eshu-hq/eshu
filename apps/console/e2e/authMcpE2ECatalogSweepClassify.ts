// authMcpE2ECatalogSweepClassify.ts — pure classification, coverage, and table
// rendering for the scoped-token full-catalog MCP sweep (issue #5167,
// acceptance: "every MCP tool succeeds with a personal token whose role covers
// it, on a live stack"). Kept free of fetch/psql/browser so
// authMcpE2ECatalogSweepClassify.test.ts can prove each verdict without a
// stack. authMcpE2ECatalogSweep.ts owns the live I/O.
import type { McpJsonRpcResult } from "./authMcpE2EJsonRpc.ts";

// SweepClass is the route class the Go route policy assigns a call
// (go/internal/mcp/dispatch_catalog_sweep_policy_test.go derives it from
// ScopedHTTPRouteSupportsTenantFilter / IsSharedKeyOnlyRoute /
// IsPendingRowFilteringRoute, never from a hand-copied list).
export type SweepClass = "allowlisted" | "shared_key_only" | "pending_row_filtering";

// SweepPolicyRow is one call the Go test emits: the tool, the route it
// dispatches to, its class, the checked-in arguments, and which outcomes the
// call may legitimately end in.
export interface SweepPolicyRow {
  readonly tool: string;
  readonly label: string;
  readonly method: string;
  readonly path: string;
  readonly class: SweepClass;
  readonly arguments: Record<string, unknown>;
  readonly accept: readonly string[];
  readonly acceptReason?: string;
}

export interface SweepPolicy {
  readonly disclosurePattern: string;
  readonly rows: readonly SweepPolicyRow[];
}

// routeDeniedMessage is the exact 403 message a ledger route answers a scoped
// token with (scopedRouteDeniedResponse in go/internal/query/auth.go). Matching
// the message, not only the status, separates "the route policy refused this
// tool" from any other permission_denied.
export const routeDeniedMessage = "scoped authorization is not yet enabled for this route";

// ROUTE_DENIED is the outcome name for a route-policy refusal.
export const ROUTE_DENIED = "route_denied_403";

interface ToolResultShape {
  readonly isError?: boolean;
  readonly content?: readonly { readonly type?: string; readonly text?: string }[];
  readonly structuredContent?: unknown;
}

// classifyToolCallOutcome reduces one tools/call response to a single outcome
// name: "ok", a typed envelope error code (not_found, invalid_argument,
// backend_unavailable, ...), ROUTE_DENIED, "route_unmounted" for a raw 404,
// or a transport-shaped name (http_<status>, jsonrpc_error). It never throws;
// an unrecognised shape becomes "unrecognized_response" so it FAILS the sweep
// instead of parsing into a false pass.
export function classifyToolCallOutcome(result: McpJsonRpcResult): { outcome: string; detail: string } {
  if (result.httpStatus !== 200) {
    return { outcome: `http_${result.httpStatus}`, detail: truncate(result.bodyText) };
  }
  if (result.json === null) {
    return { outcome: "unrecognized_response", detail: truncate(result.bodyText) };
  }
  if (result.json.error !== undefined) {
    return { outcome: "jsonrpc_error", detail: truncate(JSON.stringify(result.json.error)) };
  }
  const tool = result.json.result as ToolResultShape | undefined;
  if (tool === undefined || typeof tool !== "object") {
    return { outcome: "unrecognized_response", detail: truncate(result.bodyText) };
  }
  const text = tool.content?.find((block) => block.type === "text")?.text ?? "";
  const envelope = tool.structuredContent as { error?: { code?: string; message?: string } | null } | undefined;
  const envelopeError = envelope !== null && typeof envelope === "object" ? envelope.error : undefined;
  if (envelopeError !== undefined && envelopeError !== null) {
    const code = typeof envelopeError.code === "string" && envelopeError.code !== "" ? envelopeError.code : "envelope_error";
    const message = envelopeError.message ?? "";
    if (code === "permission_denied" && message.includes(routeDeniedMessage)) {
      return { outcome: ROUTE_DENIED, detail: message };
    }
    return { outcome: code, detail: truncate(message) };
  }
  if (tool.isError !== true) {
    return { outcome: "ok", detail: truncate(text) };
  }
  // isError with no typed envelope: mcpToolErrorResult wraps a non-envelope
  // HTTP failure as "HTTP <status>: <body>".
  const status = /^HTTP (\d{3}):/.exec(text)?.[1];
  if (status === "403" && text.includes(routeDeniedMessage)) {
    return { outcome: ROUTE_DENIED, detail: truncate(text) };
  }
  if (status === "404" && /page not found/i.test(text)) {
    return { outcome: "route_unmounted", detail: truncate(text) };
  }
  return { outcome: status ? `http_${status}` : "tool_error", detail: truncate(text) };
}

function truncate(text: string, max = 160): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
}

export interface SweepRowResult {
  readonly tool: string;
  readonly label: string;
  readonly route: string;
  readonly expected: string;
  readonly actual: string;
  readonly pass: boolean;
  readonly detail: string;
}

// expectedText renders the expectation the way the table prints it.
export function expectedText(row: SweepPolicyRow): string {
  return row.class === "allowlisted" ? `success (${row.accept.join("|")})` : `403 disclosed (${row.class})`;
}

// judgeRow applies the verdict. An allowlisted route passes only when its
// outcome is one the checked-in case accepts (default: "ok"); a ledger or
// shared-key-only route passes only when it answers the route-policy 403 AND
// its live tool description discloses the refusal. Everything else — an
// unexpected 403, a 404/unmounted route, a 5xx, an invalid-argument answer —
// fails.
export function judgeRow(
  row: SweepPolicyRow,
  outcome: { outcome: string; detail: string },
  description: string,
  disclosurePattern: RegExp,
): SweepRowResult {
  const base = { tool: row.tool, label: row.label, route: `${row.method} ${row.path}`, expected: expectedText(row) };
  if (row.class === "allowlisted") {
    const pass = row.accept.includes(outcome.outcome);
    return { ...base, actual: outcome.outcome, pass, detail: pass ? outcome.detail : `unaccepted outcome: ${outcome.detail}` };
  }
  if (outcome.outcome !== ROUTE_DENIED) {
    return { ...base, actual: outcome.outcome, pass: false, detail: `expected the route-policy 403, got: ${outcome.detail}` };
  }
  if (!disclosurePattern.test(description)) {
    return {
      ...base,
      actual: `${ROUTE_DENIED} (undisclosed)`,
      pass: false,
      detail: "the route refused the scoped token but the live tool description never says so",
    };
  }
  return { ...base, actual: `${ROUTE_DENIED} (disclosed)`, pass: true, detail: outcome.detail };
}

// coverageProblems compares the live tools/list names with the policy rows.
// A listed tool with no row means the argument table is stale (a new tool
// slipped in), and a row whose tool is not listed means the policy describes a
// tool this stack does not serve; both fail the sweep rather than shrinking it.
export function coverageProblems(listed: readonly string[], rows: readonly SweepPolicyRow[]): string[] {
  const inPolicy = new Set(rows.map((r) => r.tool));
  const live = new Set(listed);
  const problems: string[] = [];
  for (const name of [...live].sort()) {
    if (!inPolicy.has(name)) {
      problems.push(`tool ${name} is listed by tools/list but has no entry in go/internal/mcp/testdata/catalog_sweep_args.json`);
    }
  }
  for (const name of [...inPolicy].sort()) {
    if (!live.has(name)) {
      problems.push(`tool ${name} has a sweep entry but tools/list does not list it`);
    }
  }
  return problems;
}

// renderSweepTable prints the per-tool table and the pass count.
export function renderSweepTable(results: readonly SweepRowResult[]): string {
  const header = ["tool", "route", "expected", "actual", "verdict"];
  const rows = results.map((r) => [
    r.label === "default" ? r.tool : `${r.tool}[${r.label}]`,
    r.route,
    r.expected,
    r.actual,
    r.pass ? "PASS" : "FAIL",
  ]);
  const widths = header.map((h, i) => Math.max(h.length, ...rows.map((row) => row[i]!.length)));
  const line = (cells: readonly string[]): string => cells.map((c, i) => c.padEnd(widths[i]!)).join("  ").trimEnd();
  const passed = results.filter((r) => r.pass).length;
  return [line(header), line(widths.map((w) => "-".repeat(w))), ...rows.map(line), "", `${passed}/${results.length} calls passed`].join("\n");
}

// substituteSeedIds replaces the $REPO / $OTHER_REPO placeholders in a
// checked-in argument value (recursively) with the seeded repository ids.
export function substituteSeedIds(value: unknown, ids: { readonly granted: string; readonly ungranted: string }): unknown {
  if (typeof value === "string") {
    return value.split("$OTHER_REPO").join(ids.ungranted).split("$REPO").join(ids.granted);
  }
  if (Array.isArray(value)) {
    return value.map((v) => substituteSeedIds(v, ids));
  }
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, substituteSeedIds(v, ids)]));
  }
  return value;
}
