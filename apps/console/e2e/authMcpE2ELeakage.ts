// authMcpE2ELeakage.ts — negative-leakage module for the MCP-identity E2E
// suite (F-9, issue #5170, design §5). Runs LAST (after shapes A/C/B), so the
// OIDC provider is active (required for the oidcbearer denial matrix) and both
// credential types exist: shape A's AllScopes personal token and shape B's
// scoped OAuth bearer.
//
// Four proofs:
//   1. Credential-less initialize/tools-list/ping/GET-sse -> 401, with no tool
//      names / serverInfo / capabilities / jsonrpc result leaked in any body.
//   2. Distinct bad-credential denials (authMcpE2ELeakageDenials.ts) — via the
//      resolver's structured-log outcomes + F-2 challenge shapes, because
//      denial-side governance_audit reason_codes do not exist yet
//      (eshu-hq/eshu#5567).
//   3. Non-vacuous cross-scope row filter: the AllScopes personal token sees
//      the seeded Repository node; the scoped OAuth bearer (empty repo grant)
//      sees zero. Same underlying node, opposite visibility by grant.
//   4. Raw-token-absence scan across mcp-server + eshu + mock-github container
//      logs, governance_audit rows, and the admin DOM — proving no personal
//      token, OAuth JWT, or provider client secret reaches an operator-visible
//      surface. The resolver's own denial logs carry only sha256: subject
//      hashes, so a clean scan doubles as no-leak evidence for those.
import type { Page } from "playwright";

import { apiFetchInPage } from "./authE2EOidcFlow.ts";
import {
  assertProbesNonEmpty,
  collectApiContainerLogs,
  findLeakedProbes,
  scanSurfacesForLeakage,
  type LeakageSurface,
  type SecretProbe,
} from "./authE2ELeakage.ts";
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import { collectComposeServiceLogs, SEEDED_REPOSITORY_ID } from "./authMcpE2EGraphSeed.ts";
import { extractToolCallStructuredContent, mcpInitialize, mcpPing, mcpToolsCall, mcpToolsList } from "./authMcpE2EJsonRpc.ts";
import { runDistinctDenialMatrix, type DenialMatrixContext } from "./authMcpE2ELeakageDenials.ts";
import { runPsql } from "./authMcpE2EPsql.ts";

// forbiddenInDenialBody are strings that must never appear in a credential-less
// 401 body: tool names, server identity, protocol envelope. Mirrors
// go/internal/mcp/server_transport_auth_test.go's own leakage guard list.
const forbiddenInDenialBody = [
  "eshu-mcp-server",
  "serverInfo",
  "protocolVersion",
  "capabilities",
  "find_code",
  "list_indexed_repositories",
  '"result"',
  '"tools"',
];

export interface LeakageContext {
  readonly mcpBase: string;
  readonly apiBase: string;
  readonly repoRoot: string;
  readonly project: string;
  readonly hostRewrite: DenialMatrixContext["hostRewrite"];
}

// assertCredentialLessProbesDoNotLeak is exported so the mutation-sensitivity
// script (step 8) can run it standalone — it needs no browser, wizard, token,
// or shape state, only a live mcp-server + API. Against a mutated
// (ESHU_MCP_ALLOW_UNAUTHENTICATED=true) mcp-server it FAILS, because
// credential-less initialize/tools-list then return 200 instead of 401 — the
// inverted-exit signal the sensitivity gate asserts.
export async function assertCredentialLessProbesDoNotLeak(ctx: LeakageContext): Promise<string> {
  const bodies: { label: string; status: number; body: string }[] = [];

  for (const probe of [
    { label: "initialize", run: () => mcpInitialize(ctx.mcpBase, undefined) },
    { label: "tools/list", run: () => mcpToolsList(ctx.mcpBase, undefined) },
    { label: "ping", run: () => mcpPing(ctx.mcpBase, undefined) },
  ]) {
    const res = await probe.run();
    if (res.httpStatus !== 401) {
      throw new Error(`credential-less ${probe.label} expected 401, got ${res.httpStatus}: ${res.bodyText}`);
    }
    bodies.push({ label: probe.label, status: res.httpStatus, body: res.bodyText });
  }

  // GET /sse must 401 before establishing a session (no endpoint event, no
  // session id).
  const sseRes = await fetch(`${ctx.mcpBase}/sse`);
  const sseBody = await sseRes.text();
  if (sseRes.status !== 401) {
    throw new Error(`credential-less GET /sse expected 401, got ${sseRes.status}`);
  }
  if (sseBody.includes("event: endpoint") || sseBody.includes("sessionId=")) {
    throw new Error(`GET /sse 401 body leaked an SSE session/endpoint: ${sseBody}`);
  }
  bodies.push({ label: "GET /sse", status: sseRes.status, body: sseBody });

  // The API service's unauthenticated read parity check.
  const apiRes = await fetch(`${ctx.apiBase}/api/v0/repositories`);
  const apiBody = await apiRes.text();
  if (apiRes.status !== 401) {
    throw new Error(`unauthenticated GET /api/v0/repositories expected 401, got ${apiRes.status}`);
  }
  bodies.push({ label: "GET /api/v0/repositories", status: apiRes.status, body: apiBody });

  for (const { label, body } of bodies) {
    for (const forbidden of forbiddenInDenialBody) {
      if (body.includes(forbidden)) {
        throw new Error(`credential-less ${label} 401 body leaked ${forbidden}: ${body}`);
      }
    }
  }
  return `${bodies.length} credential-less probes (initialize/tools-list/ping/GET-sse/API) all 401'd with no tool/server/protocol leakage`;
}

// assertCrossScopeRowFilter proves the scoped-token repository filter is real
// and non-vacuous: the SAME seeded graph node is visible to an AllScopes
// reader and filtered out for a scoped (empty-grant) bearer. The AllScopes
// side must be a COOKIE session (ssoAdminPage) — no MCP BEARER credential in
// this stack is AllScopes (personal identity tokens and OIDC bearers both
// resolve to AuthModeScoped with an empty grant; verified live). The
// SSO-admin session is external_oidc_user with all_scopes=true and survives
// the require_sso flip, so it is the live AllScopes reader here.
async function assertCrossScopeRowFilter(
  ctx: LeakageContext,
  ssoAdminPage: Page,
  scopedBearer: string,
): Promise<string> {
  // AllScopes cookie session (via the same RepositoryHandler the MCP tool
  // dispatches to): sees the seeded node.
  const allScopesRead = await apiFetchInPage(ssoAdminPage, "GET", "/api/v0/repositories?limit=50&offset=0");
  if (allScopesRead.status !== 200) {
    throw new Error(`AllScopes GET /api/v0/repositories expected 200, got ${allScopesRead.status}: ${allScopesRead.text}`);
  }
  const allScopesBody = JSON.parse(allScopesRead.text || "{}") as { repositories?: readonly { id?: string }[] };
  const seenIds = (allScopesBody.repositories ?? []).map((r) => r.id);
  if (!seenIds.includes(SEEDED_REPOSITORY_ID)) {
    throw new Error(
      `AllScopes session did not see the seeded repository ${SEEDED_REPOSITORY_ID}; saw ids: ${JSON.stringify(seenIds)}`,
    );
  }

  // Scoped OAuth bearer (member -> owner -> empty repo grant): sees zero of
  // the same graph via the MCP tool.
  const scopedCall = await mcpToolsCall(ctx.mcpBase, "list_indexed_repositories", { limit: 50, offset: 0 }, scopedBearer);
  if (scopedCall.httpStatus !== 200) {
    throw new Error(`scoped list_indexed_repositories expected 200, got ${scopedCall.httpStatus}: ${scopedCall.bodyText}`);
  }
  const scopedResult = extractToolCallStructuredContent(scopedCall) as {
    repositories?: readonly { id?: string }[];
  };
  const scopedRows = scopedResult.repositories ?? [];
  if (scopedRows.length !== 0) {
    throw new Error(
      `scoped bearer saw ${scopedRows.length} repository row(s) — the seeded node must be filtered out for an empty-grant scope: ${JSON.stringify(scopedRows.map((r) => r.id))}`,
    );
  }
  return `non-vacuous row filter: AllScopes session sees ${SEEDED_REPOSITORY_ID}; scoped (empty-grant) bearer sees 0 of the same graph`;
}

// dumpGovernanceAuditRows returns every governance_audit_events row as
// tuples-only text, for the raw-token-absence scan (the rows carry only
// hashes, so they must be clean).
async function dumpGovernanceAuditRows(repoRoot: string, project: string): Promise<string> {
  return runPsql(
    repoRoot,
    project,
    "SELECT event_id, event_type, actor_class, COALESCE(actor_id_hash,''), decision, reason_code, " +
      "COALESCE(tenant_id,''), COALESCE(policy_revision_hash,'') FROM governance_audit_events;",
  );
}

async function assertNoRawTokenLeakage(
  ctx: LeakageContext,
  ssoAdminPage: Page,
  probes: readonly SecretProbe[],
): Promise<string> {
  assertProbesNonEmpty(probes);

  const surfaces: LeakageSurface[] = [];
  for (const service of ["mcp-server", "mock-github"] as const) {
    const body = await collectComposeServiceLogs(ctx.repoRoot, ctx.project, service);
    surfaces.push({ name: `${service} container logs`, body });
  }
  surfaces.push({ name: "eshu container logs", body: await collectApiContainerLogs(ctx.repoRoot, ctx.project) });
  surfaces.push({ name: "governance_audit_events rows", body: await dumpGovernanceAuditRows(ctx.repoRoot, ctx.project) });
  surfaces.push({ name: "admin DOM", body: await ssoAdminPage.content() });

  // Fail closed: the log/DOM surfaces must have content, or an empty read
  // would "prove" absence for the wrong reason.
  const empties = surfaces
    .filter((s) => ["mcp-server container logs", "eshu container logs", "admin DOM"].includes(s.name) && s.body.trim().length === 0)
    .map((s) => s.name);
  if (empties.length > 0) {
    throw new Error(`leakage scan read empty critical surface(s), proving nothing: ${empties.join(", ")}`);
  }

  const findings = scanSurfacesForLeakage(surfaces, probes);
  if (findings.length > 0) {
    throw new Error(`secret leakage detected: ${findings.join("; ")}`);
  }

  // The oidcbearer denial logs are the distinct-denial evidence source; confirm
  // they carry only a hashed subject shape, never a raw token (belt-and-braces
  // over the general scan above).
  const mcpLogs = surfaces.find((s) => s.name === "mcp-server container logs")!.body;
  const deniedTokenLeak = findLeakedProbes(mcpLogs, probes);
  if (deniedTokenLeak.length > 0) {
    throw new Error(`mcp-server denial logs leaked raw secret(s): ${deniedTokenLeak.join(", ")}`);
  }
  return `no raw token/secret leaked across ${surfaces.length} surfaces (mcp-server/mock-github/eshu logs, audit rows, admin DOM); ${probes.length} secrets checked`;
}

export interface LeakageInputs {
  readonly personalToken: string;
  readonly scopedBearer: string;
  // ssoAdminPage is the AllScopes external-OIDC-admin session shape B leaves
  // open (its local wizard-admin session is dead after the require_sso flip).
  readonly ssoAdminPage: Page;
}

// runLeakageSuite runs every leakage_* step in order.
//
// The design's revoked-token matrix row is intentionally OMITTED here:
// (1) token revocation is the identity-token resolver's path, not oidcbearer,
// so it produces no oidcbearer outcome — it is not part of the distinct-
// outcome set this module proves; and (2) revocation is owner-self-scoped, so
// only the wizard-admin's own session could revoke its token, and that session
// is revoked by shape B's require_sso flip before this module runs. The
// distinct-denial proof stands on three oidcbearer outcomes + F-2 challenge
// shapes + the credential-less matrix. Denial-side governance audit (which
// would give revocation a first-class distinct reason) is tracked at
// eshu-hq/eshu#5567.
export async function runLeakageSuite(step: AuthE2EStep, ctx: LeakageContext, inputs: LeakageInputs): Promise<void> {
  await step("leakage_credentialless_probes_do_not_leak", () => assertCredentialLessProbesDoNotLeak(ctx));

  await step("leakage_cross_scope_row_filter_non_vacuous", () =>
    assertCrossScopeRowFilter(ctx, inputs.ssoAdminPage, inputs.scopedBearer),
  );

  await runDistinctDenialMatrix(step, {
    mcpBase: ctx.mcpBase,
    apiBase: ctx.apiBase,
    repoRoot: ctx.repoRoot,
    project: ctx.project,
    hostRewrite: ctx.hostRewrite,
  });

  await step("leakage_no_raw_token_leakage", () =>
    assertNoRawTokenLeakage(ctx, inputs.ssoAdminPage, [
      { label: "personal API token", value: inputs.personalToken },
      { label: "OAuth JWT access token", value: inputs.scopedBearer },
      { label: "member provider client secret", value: "unused-member-provider-secret-e2e" },
      { label: "github provider client secret", value: "unused-github-provider-secret-e2e" },
    ]),
  );
}
