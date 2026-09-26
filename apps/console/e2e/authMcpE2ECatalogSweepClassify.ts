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
  // disclosurePattern is Go/JavaScript-portable regex source with no inline
  // flags; disclosureFlags carries the RegExp flags (Go applies them as (?i)).
  readonly disclosurePattern: string;
  readonly disclosureFlags: string;
  readonly rows: readonly SweepPolicyRow[];
}

// routeDeniedMessage is the exact 403 message a ledger route answers a scoped
// token with (scopedRouteDeniedResponse in go/internal/query/auth.go). Matching
// the message, not only the status, separates "the route policy refused this
// tool" from any other permission_denied.
export const routeDeniedMessage = "scoped authorization is not yet enabled for this route";

// ASK_DEFAULT_OFF is the outcome name for the ask route's default-off answer.
export const ASK_DEFAULT_OFF = "ask_default_off";

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
  // A handler's typed not-found is JSON with error "Not Found" ({"detail":...,
  // "error":"Not Found"}); an unmounted route is the mux's plain-text "404 page
  // not found" handled above. Only the JSON shape counts as an answer.
  if (status === "404" && /"error"\s*:\s*"Not Found"/.test(text)) {
    return { outcome: "not_found", detail: truncate(text) };
  }
  // ask's default-off answer is a 503 whose body says ask is not enabled
  // (askUnavailableResponse in go/internal/query/ask/handler.go). A 503 from a
  // broken backend or a failed engine carries a different reason, so it stays
  // http_503 and fails the sweep instead of passing as "default-off".
  if (status === "503" && /"state"\s*:\s*"unavailable"/.test(text) && /ask is not enabled/.test(text)) {
    return { outcome: ASK_DEFAULT_OFF, detail: truncate(text) };
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

// substituteSeedIds replaces the $REPO / $OTHER_REPO / $SCOPE / $STATE_SCOPE placeholders in a
// checked-in argument value (recursively) with the seeded repository ids.
export function substituteSeedIds(
  value: unknown,
  ids: { readonly granted: string; readonly ungranted: string; readonly scope: string; readonly stateScope: string },
): unknown {
  if (typeof value === "string") {
    return value
      .split("$OTHER_REPO").join(ids.ungranted)
      .split("$STATE_SCOPE").join(ids.stateScope)
      .split("$SCOPE").join(ids.scope)
      .split("$REPO").join(ids.granted);
  }
  if (Array.isArray(value)) {
    return value.map((v) => substituteSeedIds(v, ids));
  }
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, substituteSeedIds(v, ids)]));
  }
  return value;
}

// compileDisclosurePattern builds the RegExp the Go test's policy describes.
// It is the single place the emitted pattern becomes a JavaScript RegExp, so a
// Go-only construct in the pattern fails here with a message naming the policy
// rather than as a bare "Invalid group" mid-sweep.
export function compileDisclosurePattern(policy: Pick<SweepPolicy, "disclosurePattern" | "disclosureFlags">): RegExp {
  try {
    return new RegExp(policy.disclosurePattern, policy.disclosureFlags);
  } catch (err) {
    throw new Error(
      `policy disclosurePattern ${JSON.stringify(policy.disclosurePattern)} is not a valid JavaScript RegExp ` +
        `(flags ${JSON.stringify(policy.disclosureFlags)}): ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

// SeedIds are the seeded subjects the sweep substitutes for placeholders.
export interface SeedIds {
  readonly granted: string;
  readonly ungranted: string;
  readonly scope: string;
  readonly stateScope: string;
}

// AllScopeControl is the all-scope replay of one tolerant row.
export interface AllScopeControl {
  readonly method: string;
  readonly path: string;
  readonly body: Record<string, unknown> | undefined;
}

// allScopeControlFor decides whether a row needs an all-scope control and, when
// it does, builds the replay. A tolerant allowlisted row that passed the GRANTED
// repository and answered not_found cannot, from the scoped answer alone, be
// told apart from a grant filter that wrongly hid the granted repository; the
// same call through the all-scope session must answer the same not-found to
// prove the fixture (not the grant) is missing the subject. It returns
// undefined for a row that answered anything but not_found, is not
// allowlisted, does not accept not_found, or does not name the granted
// repository.
export function allScopeControlFor(
  row: SweepPolicyRow,
  actual: string,
  ids: SeedIds,
): AllScopeControl | undefined {
  if (row.class !== "allowlisted" || actual !== "not_found" || !row.accept.includes("not_found")) {
    return undefined;
  }
  const args = substituteSeedIds(row.arguments, ids) as Record<string, unknown>;
  if (!JSON.stringify(args).includes(JSON.stringify(ids.granted).slice(1, -1))) {
    return undefined;
  }
  const path = substituteSeedIds(row.path, ids) as string;
  return { method: row.method, path, body: row.method === "GET" ? undefined : args };
}

// judgeAllScopeControl accepts the control only when the all-scope session gets
// the same typed not-found (HTTP 404 with the handler's "Not Found" body, not
// the mux's unmounted-route page). A 200 means the grant, not the fixture,
// hid the subject.
export function judgeAllScopeControl(status: number, text: string): { pass: boolean; detail: string } {
  if (status === 404 && /"error"\s*:\s*"Not Found"/.test(text) && !/page not found/i.test(text)) {
    return { pass: true, detail: "all-scope session answered the same typed not-found" };
  }
  return { pass: false, detail: `all-scope session answered ${status}, not the typed 404 not-found: ${truncate(text)}` };
}
