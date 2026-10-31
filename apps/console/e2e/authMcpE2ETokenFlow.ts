// authMcpE2ETokenFlow.ts — shape A (token-only org) step bodies for the
// MCP-identity E2E suite (F-9, issue #5170, design §4 "Shape A"). Proves the
// token-only posture end to end: zero-provider login page, static discovery
// served from boot (the satellite advertises its configured issuers
// regardless of DB provider rows — eshu-hq/eshu#9731), the console's
// self-service personal-token UI (F-3), an authenticated MCP tools/call over
// that token, that the call emits no allowed-read governance-audit event
// (the F-9 sink lived only in the retired in-tree MCP transport middleware),
// and the discovery challenge shape a static-discovery deployment sends on
// a credential-less 401.
import type { Browser, Page } from "playwright";

import { apiFetchInPage } from "./authE2EOidcFlow.ts";
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import {
  extractToolCallStructuredContent,
  mcpInitialize,
  mcpToolsCall,
  mcpToolsList,
} from "./authMcpE2EJsonRpc.ts";
import { countGovernanceAuditEvents } from "./authMcpE2EPsql.ts";
import { assertEmptyGrantRepositoryList } from "./authMcpE2ERepositoryRows.ts";

export interface ShapeAContext {
  readonly browser: Browser;
  readonly baseUrl: string;
  readonly mcpBase: string;
  readonly apiBase: string;
  readonly repoRoot: string;
  readonly project: string;
  readonly navTimeoutMs: number;
}

// driveCreatePersonalTokenViaUI drives ProfilePage's TokensSection/
// CreateApiTokenControl (issue #5164) to mint a real personal API token
// through the console UI (F-3's acceptance surface), returning the
// once-shown raw token value. Must run on an authenticated admin/member
// `page` — the control is only rendered when TokensSection receives a
// client (see TokensSection.tsx).
export async function driveCreatePersonalTokenViaUI(page: Page, label: string, navTimeoutMs: number): Promise<string> {
  await page.goto("/profile", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".token-create-start", { timeout: navTimeoutMs });
  await page.click(".token-create-start");
  await page.waitForSelector("#token-create-label", { timeout: navTimeoutMs });
  await page.fill("#token-create-label", label);
  await page.click('.token-create-actions button:has-text("Create")');
  await page.waitForSelector("#token-reveal-value", { timeout: navTimeoutMs });
  const token = await page.inputValue("#token-reveal-value");
  if (token.trim() === "") {
    throw new Error("token-reveal-value rendered but was empty after creating a personal API token");
  }
  // Ack + dismiss, mirroring a real operator's flow (also required so the
  // panel returns to the token list for any later step that re-opens /profile).
  await page.click('label[for="token-reveal-ack"]');
  await page.click(".token-reveal-done");
  await page.waitForSelector(".token-reveal", { state: "detached", timeout: navTimeoutMs });
  return token;
}

// assertLoginPageTokenOnlyPosture proves the fresh-stack login page renders
// only the local form (zero .btn-sso) — F-4's negative shape for a
// zero-provider deployment.
async function assertLoginPageTokenOnlyPosture(ctx: ShapeAContext): Promise<string> {
  const context = await ctx.browser.newContext({ baseURL: ctx.baseUrl });
  try {
    const page = await context.newPage();
    await page.goto("/login?tenant_id=default", { waitUntil: "domcontentloaded", timeout: ctx.navTimeoutMs });
    await page.waitForSelector("#login-id", { timeout: ctx.navTimeoutMs });
    const ssoCount = await page.locator(".btn-sso").count();
    if (ssoCount !== 0) {
      throw new Error(`expected zero .btn-sso buttons on a zero-provider login page, found ${ssoCount}`);
    }
    return "local form present, zero SSO buttons on /login with no providers configured";
  } finally {
    await context.close();
  }
}

// assertDiscoveryStaticFromBoot proves the satellite static-discovery
// posture (eshu-hq/eshu#9731): the edge serves the RFC 9728 document from
// boot whenever the stack configures issuers (compose
// ESHU_AUTH_AUTHORIZATION_SERVERS), independent of DB provider rows. The
// retired in-tree transport derived this per request from Postgres and 404'd
// with zero providers; the satellite cannot — its static operator config is
// the trust root. Keeps the shapeA_discovery_404 step id for the manifest
// gate; only the asserted contract changed.
async function assertDiscoveryStaticFromBoot(ctx: ShapeAContext): Promise<string> {
  const res = await fetch(`${ctx.mcpBase}/.well-known/oauth-protected-resource`);
  if (res.status !== 200) {
    throw new Error(`expected 200 from static discovery with configured issuers, got ${res.status}`);
  }
  const doc = (await res.json()) as Record<string, unknown>;
  if (doc.resource !== ctx.mcpBase) {
    throw new Error(`discovery resource = ${String(doc.resource)}, want ${ctx.mcpBase}`);
  }
  const servers = doc.authorization_servers as readonly string[] | undefined;
  if (!servers || servers.length === 0) {
    throw new Error(`discovery authorization_servers = ${JSON.stringify(servers)}, want the configured issuers`);
  }
  const bearerMethods = doc.bearer_methods_supported as readonly string[] | undefined;
  if (!bearerMethods || bearerMethods.length !== 1 || bearerMethods[0] !== "header") {
    throw new Error(`discovery bearer_methods_supported = ${JSON.stringify(bearerMethods)}, want [header]`);
  }
  return `static discovery serves 200 from boot with resource=${String(doc.resource)}, authorization_servers=[${servers.join(",")}]`;
}

// assertMcpToolCallRowFiltered drives a full initialize -> tools/list ->
// tools/call round trip authenticated with the personal token, asserting
// 200s, a genuinely non-empty tool catalog (proving auth let the call
// through, not merely that the transport accepted the credential), and — the
// load-bearing part — that the personal token sees EXACTLY ZERO repositories.
//
// This is Shape A's identity claim, and it is NON-VACUOUS: the suite seeds one
// Repository node into the graph BEFORE this step runs (runAuthMcpE2E.ts's
// seed_graph_repository), and the leakage module proves that same node IS
// visible to an AllScopes cookie session. A personal identity token resolves
// to AuthModeScoped with an EMPTY repository/scope grant
// (go/internal/scopedtoken/identity.go — no AllScopes), so
// repositoryAccessFilterFromContext fail-closes it to zero rows even though
// the graph is non-empty. Asserting rows.length === 0 here therefore catches a
// real personal-token scope-escalation regression: if the token could see the
// seeded repo, this throws (and that would be a genuine product leak, not a
// test artefact). (This replaces an earlier assertion that filtered rows by a
// `tenant_id` field — repositories carry no tenant_id, only id/name — which
// passed vacuously and probed a nonexistent field; and a later revision that
// captured rows.length into the message but never asserted it, so the isolation
// was proven nowhere.)
async function assertMcpToolCallRowFiltered(ctx: ShapeAContext, token: string): Promise<string> {
  const init = await mcpInitialize(ctx.mcpBase, token);
  if (init.httpStatus !== 200) {
    throw new Error(`initialize with personal token expected 200, got ${init.httpStatus}: ${init.bodyText}`);
  }
  const list = await mcpToolsList(ctx.mcpBase, token);
  if (list.httpStatus !== 200) {
    throw new Error(`tools/list with personal token expected 200, got ${list.httpStatus}: ${list.bodyText}`);
  }
  const toolsArray = (list.json?.result as { tools?: readonly { name?: string }[] } | undefined)?.tools ?? [];
  if (toolsArray.length === 0) {
    throw new Error(`tools/list returned zero tools authenticated with a personal token: ${list.bodyText}`);
  }
  const call = await mcpToolsCall(ctx.mcpBase, "list_indexed_repositories", { limit: 50, offset: 0 }, token);
  if (call.httpStatus !== 200) {
    throw new Error(`tools/call list_indexed_repositories expected 200, got ${call.httpStatus}: ${call.bodyText}`);
  }
  // The payload is the truth envelope: the list is at structuredContent.data.
  // assertEmptyGrantRepositoryList reads it there and fails closed on any other
  // shape (a top-level read finds no rows on every response and cannot fail).
  return `${toolsArray.length} tools listed; ${assertEmptyGrantRepositoryList(extractToolCallStructuredContent(call), "scoped personal token")}`;
}

// scopedReadAllowedFilter names the async allowed-read audit event shape A's
// MCP reads produce (F-9 part 1).
const scopedReadAllowedFilter = {
  eventType: "read_authorization",
  decision: "allowed",
  reasonCode: "scoped_read_allowed",
} as const;

// assertAllowedReadAuditNotEmitted proves the satellite-architecture audit
// contract for shape A (eshu-hq/eshu#9731): a successful satellite-forwarded
// read emits NO scoped_read_allowed event. The F-9 allowed-read sink was
// threaded only through the retired in-tree MCP transport middleware; the
// satellite owns no audit sink and the core read API its dispatches traverse
// records none (allowedAudit is nil outside that removed middleware). So the
// row-filtered tools/call here must leave the scoped_read_allowed count
// exactly unchanged. Asserting delta == 0 (not merely skipping the step)
// keeps the shapeA_allowed_read_audit_recorded step id stable for the
// manifest gate while failing loudly if emission wiring ever lands,
// deliberately, on this path.
async function assertAllowedReadAuditNotEmitted(ctx: ShapeAContext, token: string, baselineCount: number): Promise<string> {
  const call = await mcpToolsCall(ctx.mcpBase, "list_indexed_repositories", { limit: 50, offset: 0 }, token);
  if (call.httpStatus !== 200) {
    throw new Error(`tools/call list_indexed_repositories expected 200, got ${call.httpStatus}: ${call.bodyText}`);
  }
  // The retired F-9 sink flushed asynchronously (AsyncAppender, ~2s flush
  // timeout): recount only after that window, so a re-added emission cannot
  // slip through a pre-flush read and pass vacuously.
  await new Promise((r) => setTimeout(r, 5000));
  const after = await countGovernanceAuditEvents(ctx.repoRoot, ctx.project, scopedReadAllowedFilter);
  if (after !== baselineCount) {
    throw new Error(
      `scoped_read_allowed count moved ${baselineCount} -> ${after} across a satellite-forwarded read: the satellite path must not emit allowed-read audit`,
    );
  }
  return `scoped_read_allowed count unchanged (${baselineCount}) across a satellite-forwarded read: no allowed-read emission on the satellite path`;
}

// assertCredentialLessChallengeSteersToDiscovery proves a credential-less MCP
// request on this deployment gets the RFC 9728 resource_metadata challenge,
// not the bare "Bearer" the retired in-tree transport sent with zero
// providers. The satellite enables discovery statically from operator env
// (compose ESHU_AUTH_AUTHORIZATION_SERVERS), so every credential-less 401
// steers discovery-capable clients at the metadata document. Keeps the
// shapeA_credentialless_challenge_is_bare step id for the manifest gate.
async function assertCredentialLessChallengeSteersToDiscovery(ctx: ShapeAContext): Promise<string> {
  const res = await mcpInitialize(ctx.mcpBase, undefined);
  if (res.httpStatus !== 401) {
    throw new Error(`expected 401 from credential-less initialize, got ${res.httpStatus}`);
  }
  if (res.wwwAuthenticate === null || !res.wwwAuthenticate.includes("resource_metadata=")) {
    throw new Error(`expected the resource_metadata challenge on a credential-less 401, got: ${res.wwwAuthenticate}`);
  }
  return `credential-less initialize 401'd with the discovery challenge: ${res.wwwAuthenticate}`;
}

// runShapeA runs every shapeA_* step against the given wizard-admin page and
// context, in the order design §4 specifies. adminPage must already be an
// authenticated console session (the post-setup-wizard admin). Returns the
// minted personal token so later shapes (C's "MCP still works via the
// existing personal token" reconfirmation) can reuse it without minting a
// second one.
export async function runShapeA(step: AuthE2EStep, adminPage: Page, ctx: ShapeAContext): Promise<string> {
  await step("shapeA_login_page_token_only_posture", () => assertLoginPageTokenOnlyPosture(ctx));
  await step("shapeA_discovery_404", () => assertDiscoveryStaticFromBoot(ctx));

  let personalToken = "";
  await step("shapeA_console_personal_token_created", async () => {
    personalToken = await driveCreatePersonalTokenViaUI(adminPage, "e2e-shapeA-token", ctx.navTimeoutMs);
    return `personal API token minted via /profile's Create-token control (${personalToken.length} chars)`;
  });

  // Capture the DB-side baseline count of scoped_read_allowed events BEFORE the
  // reads, so the no-emission assertion below compares a count delta (0 on
  // the satellite path) rather than occurred_at against a host wall-clock —
  // immune to host/container clock skew.
  const baselineAuditCount = await countGovernanceAuditEvents(ctx.repoRoot, ctx.project, scopedReadAllowedFilter);
  await step("shapeA_mcp_tool_call_row_filtered", () => assertMcpToolCallRowFiltered(ctx, personalToken));
  await step("shapeA_allowed_read_audit_recorded", () =>
    assertAllowedReadAuditNotEmitted(ctx, personalToken, baselineAuditCount),
  );
  await step("shapeA_credentialless_challenge_is_bare", () => assertCredentialLessChallengeSteersToDiscovery(ctx));

  // Register the token for the negative-leakage scan's raw-token-absence grep
  // (design §5) via the shared apiFetchInPage-backed profile read, proving the
  // console itself never echoes it back on a subsequent list call.
  const listResult = await apiFetchInPage(adminPage, "GET", "/api/v0/auth/local/api-tokens");
  if (listResult.status !== 200) {
    throw new Error(`GET /api/v0/auth/local/api-tokens after creation expected 200, got ${listResult.status}`);
  }
  if (listResult.text.includes(personalToken)) {
    throw new Error("GET /api/v0/auth/local/api-tokens echoed the raw personal token back");
  }
  return personalToken;
}

export { assertMcpToolCallRowFiltered };
