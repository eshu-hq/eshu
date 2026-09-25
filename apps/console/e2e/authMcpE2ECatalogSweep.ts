// authMcpE2ECatalogSweep.ts — the scoped-token full-catalog MCP sweep (issue
// #5167 acceptance: "every MCP tool succeeds with a personal token whose role
// covers it, on a live stack"; the other half is that a ledger route refuses a
// scoped token and says why).
//
// Run with `scripts/run-auth-mcp-e2e.sh --module catalog-sweep`. The module:
//   1. loads the tool -> route -> class policy the Go test
//      TestCatalogSweepPolicy (go/internal/mcp) derived from the route-policy
//      predicates, so the expectation cannot drift from the Go source of truth;
//   2. seeds a role whose grants cover every feature/data class plus a
//      repository target for ONE seeded repository, and a second, ungranted
//      repository, then mints a real personal token through the console UI;
//   3. calls tools/list with that scoped token and every listed tool with the
//      checked-in minimal arguments, classifies each result, and prints a
//      per-tool table;
//   4. runs a negative control: the same token, an allowlisted tool, and the
//      ungranted repository's id must return nothing from that repository.
//
// The module is opt-in (never part of the empty-selector full run): it makes
// ~170 tool calls and needs no OIDC/GitHub shape state, only the bootstrap
// admin session the runner already opens.
import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import type { Page } from "playwright";

import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import {
  classifyToolCallOutcome,
  compileDisclosurePattern,
  coverageProblems,
  judgeRow,
  renderSweepTable,
  substituteSeedIds,
  type SweepPolicy,
  type SweepRowResult,
} from "./authMcpE2ECatalogSweepClassify.ts";
import { SEEDED_REPOSITORY_ID, seedGraphRepository } from "./authMcpE2EGraphSeed.ts";
import { mcpToolsCall, mcpToolsList } from "./authMcpE2EJsonRpc.ts";
import { runPsql, splitPsqlRows } from "./authMcpE2EPsql.ts";
import { driveCreatePersonalTokenViaUI } from "./authMcpE2ETokenFlow.ts";

// UNGRANTED_REPOSITORY_ID is the second seeded Repository node. The sweep's
// role grants read on SEEDED_REPOSITORY_ID only, so this one must stay
// invisible to the scoped token.
export const UNGRANTED_REPOSITORY_ID = "e2e-seed-repo-ungranted";

const sweepRoleId = "e2e_catalog_sweep_reader";
const sweepScopeId = "e2e-catalog-sweep-scope-granted";

export interface CatalogSweepContext {
  readonly mcpBase: string;
  readonly apiBase: string;
  readonly repoRoot: string;
  readonly project: string;
  readonly nornicHttpBase: string;
  readonly navTimeoutMs: number;
  readonly artifactsDir: string;
  // policyPath is the JSON TestCatalogSweepPolicy wrote (ESHU_E2E_CATALOG_SWEEP_POLICY).
  readonly policyPath: string;
}

const sqlQuote = (value: string): string => `'${value.replace(/'/g, "''")}'`;

// loadSweepPolicy reads and shape-checks the Go-emitted policy. A missing or
// empty file fails loudly: a sweep with no rows would otherwise green.
export async function loadSweepPolicy(path: string): Promise<SweepPolicy> {
  if (path === "") {
    throw new Error(
      "ESHU_E2E_CATALOG_SWEEP_POLICY is unset; run through scripts/run-auth-mcp-e2e.sh --module catalog-sweep, " +
        "which derives the policy with `go test ./internal/mcp -run TestCatalogSweepPolicy`",
    );
  }
  const policy = JSON.parse(await readFile(path, "utf8")) as SweepPolicy;
  if (!Array.isArray(policy.rows) || policy.rows.length === 0 || typeof policy.disclosurePattern !== "string") {
    throw new Error(`${path} is not a catalog-sweep policy (no rows or no disclosurePattern)`);
  }
  compileDisclosurePattern(policy);
  return policy;
}

// seedSweepGrants gives the bootstrap admin user one extra role that (a) grants
// every permission feature and data class, so the permission catalog never
// refuses a tool for want of a role, and (b) targets exactly one repository, so
// the resulting personal token is AuthModeScoped with a one-repository grant.
// No product API writes role grants or repository targets (only tests and the
// OIDC/GitHub mappers do), so this uses the same direct-psql seed the rest of
// the suite uses for fixtures the product has no write surface for.
async function seedSweepGrants(ctx: CatalogSweepContext): Promise<string> {
  const ownerRows = splitPsqlRows(
    await runPsql(
      ctx.repoRoot,
      ctx.project,
      "SELECT tenant_id, workspace_id, user_id, policy_revision_hash FROM identity_membership_roles " +
        "WHERE role_id = 'owner' AND status = 'active' AND tombstoned_at IS NULL ORDER BY created_at LIMIT 1;",
    ),
  );
  const owner = ownerRows[0];
  if (owner === undefined || owner.length < 4) {
    throw new Error("no active owner membership found; the bootstrap wizard did not create the admin user");
  }
  const [tenant, workspace, user, revision] = owner.map((v) => sqlQuote(v)) as [string, string, string, string];
  const role = sqlQuote(sweepRoleId);
  const scope = sqlQuote(sweepScopeId);
  const granted = sqlQuote(SEEDED_REPOSITORY_ID);
  // One multi-statement -c runs as a single implicit transaction. The policy
  // revision must equal the owner assignment's: identity token resolution
  // fails closed when a token's roles carry more than one revision.
  const sql = [
    `INSERT INTO identity_roles (tenant_id, role_id, role_key_hash, status, built_in, policy_revision_hash, created_at, updated_at) VALUES (${tenant}, ${role}, ${sqlQuote(`sha256:${sweepRoleId}`)}, 'active', false, ${revision}, now(), now())`,
    `INSERT INTO identity_role_grants (tenant_id, role_id, grant_id, action, feature, data_class, scope_class, status, policy_revision_hash, effective_at, created_at, updated_at) VALUES (${tenant}, ${role}, 'grant-all-read', 'read', '*', '*', 'repository', 'active', ${revision}, now(), now(), now())`,
    `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status) VALUES (${scope}, 'repository', 'git', ${granted}, 'git', ${granted}, now(), now(), 'active')`,
    `INSERT INTO identity_role_scope_targets (tenant_id, workspace_id, role_id, scope_id, status, grant_source, policy_revision_hash, effective_at, created_at, updated_at) VALUES (${tenant}, ${workspace}, ${role}, ${scope}, 'active', 'e2e_catalog_sweep', ${revision}, now(), now(), now())`,
    `INSERT INTO identity_role_repository_targets (tenant_id, workspace_id, role_id, repo_id, scope_id, status, grant_source, policy_revision_hash, effective_at, created_at, updated_at) VALUES (${tenant}, ${workspace}, ${role}, ${granted}, ${scope}, 'active', 'e2e_catalog_sweep', ${revision}, now(), now(), now())`,
    `INSERT INTO identity_membership_roles (tenant_id, workspace_id, user_id, role_id, assignment_source, status, policy_revision_hash, effective_at, created_at, updated_at) VALUES (${tenant}, ${workspace}, ${user}, ${role}, 'e2e_catalog_sweep', 'active', ${revision}, now(), now(), now())`,
  ].join("; ");
  await runPsql(ctx.repoRoot, ctx.project, `${sql};`);
  await seedGraphRepository(ctx.nornicHttpBase, UNGRANTED_REPOSITORY_ID);
  return `role ${sweepRoleId} (all features, all data classes) granted on repository ${SEEDED_REPOSITORY_ID} only; ungranted repository ${UNGRANTED_REPOSITORY_ID} seeded into the graph`;
}

// assertTokenIsScoped proves the credential the sweep uses is a personal token
// resolved through roles (permission_catalog_enforced=true, carrying the sweep
// role), not the shared key. This stack boots with no shared ESHU_API_KEY, so
// a call that succeeds can only have authenticated as an identity credential;
// the profile read makes that explicit instead of implied.
async function assertTokenIsScoped(ctx: CatalogSweepContext, token: string): Promise<string> {
  const res = await fetch(`${ctx.apiBase}/api/v0/auth/profile`, { headers: { Authorization: `Bearer ${token}` } });
  const text = await res.text();
  if (res.status !== 200) {
    throw new Error(`GET /api/v0/auth/profile with the personal token expected 200, got ${res.status}: ${text}`);
  }
  const profile = JSON.parse(text) as { role_ids?: readonly string[]; permission_catalog_enforced?: boolean };
  if (profile.permission_catalog_enforced !== true) {
    throw new Error(`personal token is not permission-catalog enforced (shared-key-like): ${text}`);
  }
  if (!(profile.role_ids ?? []).includes(sweepRoleId)) {
    throw new Error(`personal token does not carry role ${sweepRoleId}: ${text}`);
  }
  return `token resolves through roles ${JSON.stringify(profile.role_ids)} with the permission catalog enforced`;
}

interface ListedTool {
  readonly name: string;
  readonly description: string;
}

async function listTools(ctx: CatalogSweepContext, token: string): Promise<ListedTool[]> {
  const list = await mcpToolsList(ctx.mcpBase, token);
  if (list.httpStatus !== 200) {
    throw new Error(`tools/list with the scoped token expected 200, got ${list.httpStatus}: ${list.bodyText}`);
  }
  const tools = (list.json?.result as { tools?: readonly { name?: string; description?: string }[] } | undefined)?.tools;
  if (!Array.isArray(tools) || tools.length === 0) {
    throw new Error(`tools/list returned no tools for the scoped token: ${list.bodyText}`);
  }
  return tools.map((t) => ({ name: String(t.name ?? ""), description: String(t.description ?? "") }));
}

// sweepEveryTool calls every policy row with the scoped token and judges it.
async function sweepEveryTool(
  ctx: CatalogSweepContext,
  token: string,
  policy: SweepPolicy,
  listed: readonly ListedTool[],
): Promise<string> {
  const problems = coverageProblems(
    listed.map((t) => t.name),
    policy.rows,
  );
  if (problems.length > 0) {
    throw new Error(`catalog coverage mismatch:\n  ${problems.join("\n  ")}`);
  }
  const descriptions = new Map(listed.map((t) => [t.name, t.description]));
  const disclosure = compileDisclosurePattern(policy);
  const ids = { granted: SEEDED_REPOSITORY_ID, ungranted: UNGRANTED_REPOSITORY_ID };
  const results: SweepRowResult[] = [];
  for (const row of policy.rows) {
    const args = substituteSeedIds(row.arguments, ids) as Record<string, unknown>;
    const call = await mcpToolsCall(ctx.mcpBase, row.tool, args, token);
    results.push(judgeRow(row, classifyToolCallOutcome(call), descriptions.get(row.tool) ?? "", disclosure));
  }
  const table = renderSweepTable(results);
  process.stdout.write(`\n${table}\n\n`);
  await writeFile(resolve(ctx.artifactsDir, "auth-mcp-e2e-catalog-sweep.txt"), `${table}\n`, "utf8");
  await writeFile(
    resolve(ctx.artifactsDir, "auth-mcp-e2e-catalog-sweep.json"),
    JSON.stringify({ tools: listed.length, calls: results.length, results }, null, 2),
    "utf8",
  );
  const failed = results.filter((r) => !r.pass);
  if (failed.length > 0) {
    const lines = failed.map((r) => `${r.tool}[${r.label}] ${r.route}: expected ${r.expected}, got ${r.actual} — ${r.detail}`);
    throw new Error(`${failed.length}/${results.length} calls failed:\n  ${lines.join("\n  ")}`);
  }
  const ledger = results.filter((r) => r.expected.startsWith("403")).length;
  return `${listed.length} tools listed; ${results.length}/${results.length} calls passed (${results.length - ledger} allowlisted succeeded, ${ledger} ledger/shared-key routes refused with a disclosed 403)`;
}

// singleRepoTools are allowlisted tools that take one repo_id and answer from a
// grant-filtered selector, so an ungranted id must not be served.
const singleRepoTools = [
  "get_repo_summary",
  "get_repo_context",
  "get_repo_story",
  "get_repository_stats",
  "get_repository_coverage",
  "get_repository_freshness",
] as const;

// assertNegativeControl proves the scoped token cannot read outside its grant.
// list_indexed_repositories is the row-level proof and is non-vacuous by
// construction: both repositories exist in the graph, so exactly the granted
// one must come back. The single-repository tools are then asked for the
// ungranted id; none may answer "ok" with data. The granted id is asked too, so
// a refusal cannot be blamed on missing seed data.
async function assertNegativeControl(ctx: CatalogSweepContext, token: string): Promise<string> {
  const list = await mcpToolsCall(ctx.mcpBase, "list_indexed_repositories", { limit: 50, offset: 0 }, token);
  const listOutcome = classifyToolCallOutcome(list);
  if (listOutcome.outcome !== "ok") {
    throw new Error(`list_indexed_repositories with the scoped token was not ok: ${listOutcome.outcome} ${listOutcome.detail}`);
  }
  type RepoRows = { repositories?: readonly { id?: string }[] };
  const structured = (list.json?.result as { structuredContent?: RepoRows & { data?: RepoRows } })?.structuredContent;
  // The canonical envelope nests the payload under data; a plain-payload
  // response would carry repositories at the top level. Accept either so the
  // control never depends on which one this route negotiates, but an absent
  // list still fails below (the granted id must be present).
  const rows = (structured?.data?.repositories ?? structured?.repositories ?? []).map((r) => r.id);
  if (!rows.includes(SEEDED_REPOSITORY_ID)) {
    throw new Error(`the granted repository ${SEEDED_REPOSITORY_ID} is missing from the scoped list (${JSON.stringify(rows)}); the control would be vacuous`);
  }
  if (rows.includes(UNGRANTED_REPOSITORY_ID)) {
    throw new Error(`scope escape: the scoped token listed ungranted repository ${UNGRANTED_REPOSITORY_ID} (${JSON.stringify(rows)})`);
  }
  const perTool: string[] = [];
  let groundedByGrantedOk = 0;
  for (const tool of singleRepoTools) {
    const denied = classifyToolCallOutcome(await mcpToolsCall(ctx.mcpBase, tool, { repo_id: UNGRANTED_REPOSITORY_ID }, token));
    if (denied.outcome === "ok") {
      throw new Error(`scope escape: ${tool} answered ok for ungranted repository ${UNGRANTED_REPOSITORY_ID}: ${denied.detail}`);
    }
    const allowed = classifyToolCallOutcome(await mcpToolsCall(ctx.mcpBase, tool, { repo_id: SEEDED_REPOSITORY_ID }, token));
    if (allowed.outcome === "ok") {
      groundedByGrantedOk += 1;
    }
    perTool.push(`${tool}: ungranted=${denied.outcome}, granted=${allowed.outcome}`);
  }
  if (groundedByGrantedOk === 0) {
    throw new Error(
      `no single-repository tool answered ok for the granted repository, so the ungranted refusals prove nothing:\n  ${perTool.join("\n  ")}`,
    );
  }
  return `scoped list = ${JSON.stringify(rows)} (ungranted absent); ${perTool.join("; ")}`;
}

// runCatalogSweep runs every catalog-sweep_* step on the bootstrapped admin page.
export async function runCatalogSweep(step: AuthE2EStep, adminPage: Page, ctx: CatalogSweepContext): Promise<void> {
  let policy: SweepPolicy | undefined;
  await step("catalog-sweep_policy_loaded", async () => {
    policy = await loadSweepPolicy(ctx.policyPath);
    const tools = new Set(policy.rows.map((r) => r.tool)).size;
    return `${policy.rows.length} calls across ${tools} tools, classified by the Go route policy`;
  });
  if (policy === undefined) {
    return;
  }
  const loaded = policy;

  await step("catalog-sweep_grant_and_ungranted_repo_seeded", () => seedSweepGrants(ctx));

  let token = "";
  await step("catalog-sweep_personal_token_minted", async () => {
    token = await driveCreatePersonalTokenViaUI(adminPage, "e2e-catalog-sweep-token", ctx.navTimeoutMs);
    return `personal API token minted via /profile (${token.length} chars)`;
  });
  if (token === "") {
    return;
  }

  await step("catalog-sweep_token_is_scoped_not_shared", () => assertTokenIsScoped(ctx, token));
  const listed: ListedTool[] = [];
  await step("catalog-sweep_tools_list", async () => {
    listed.push(...(await listTools(ctx, token)));
    return `${listed.length} tools listed to the scoped token`;
  });
  if (listed.length === 0) {
    return;
  }
  await step("catalog-sweep_every_tool_matches_route_policy", () => sweepEveryTool(ctx, token, loaded, listed));
  await step("catalog-sweep_negative_control_ungranted_repo_unreadable", () => assertNegativeControl(ctx, token));
}
