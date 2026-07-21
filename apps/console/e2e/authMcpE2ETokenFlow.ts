// authMcpE2ETokenFlow.ts — shape A (token-only org) step bodies for the
// MCP-identity E2E suite (F-9, issue #5170, design §4 "Shape A"). Proves the
// token-only posture end to end: zero-provider login page, 404 discovery, the
// console's self-service personal-token UI (F-3), an authenticated MCP
// tools/call over that token, the F-9-part-1 allowed-read governance-audit
// event landing (bounded async poll), and the bare (non-OAuth) 401 challenge
// shape a zero-provider deployment must keep.
import type { Browser, Page } from "playwright";

import { apiFetchInPage } from "./authE2EOidcFlow.ts";
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import {
  extractToolCallStructuredContent,
  mcpInitialize,
  mcpToolsCall,
  mcpToolsList,
} from "./authMcpE2EJsonRpc.ts";
import { pollForGovernanceAuditEvent } from "./authMcpE2EPsql.ts";

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

// assertDiscoveryRoute404 proves F-2's "indistinguishable from route not
// mounted" contract on a zero-provider deployment: the RFC 9728 document
// itself 404s, and the body carries neither RFC 9728 key.
async function assertDiscoveryRoute404(ctx: ShapeAContext): Promise<string> {
  const res = await fetch(`${ctx.mcpBase}/.well-known/oauth-protected-resource`);
  if (res.status !== 404) {
    throw new Error(`expected 404 from discovery with zero providers, got ${res.status}`);
  }
  const bodyText = await res.text();
  for (const forbidden of ["authorization_servers", '"resource"']) {
    if (bodyText.includes(forbidden)) {
      throw new Error(`404 discovery body unexpectedly carries RFC 9728 key ${forbidden}: ${bodyText}`);
    }
  }
  return "GET /.well-known/oauth-protected-resource returned 404 with no RFC 9728 keys leaked";
}

// assertMcpToolCallRowFiltered drives a full initialize -> tools/list ->
// tools/call round trip authenticated with the personal token, asserting
// 200s, a genuinely non-empty tool catalog (proving auth let the call
// through, not merely that the transport accepted the credential), and a
// well-formed (bounded) tool result.
//
// It does NOT assert the personal token SEES any repository: a personal
// identity token resolves to AuthModeScoped with an EMPTY repository/scope
// grant (go/internal/scopedtoken/identity.go — no AllScopes), so
// repositoryAccessFilterFromContext fail-closes it to zero rows regardless of
// corpus (verified live). The non-vacuous row-filter proof — an AllScopes
// cookie session seeing the seeded node while this scoped bearer sees zero —
// lives in the leakage module, which has both credentials on hand. (This
// replaces an earlier assertion that filtered rows by a `tenant_id` field:
// repositories carry no tenant_id, only id/name, so that check both passed
// vacuously and probed a nonexistent field — the isolation dimension is the
// scoped repository/scope grant, not tenant.)
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
  const parsed = extractToolCallStructuredContent(call) as {
    repositories?: readonly { id?: string }[];
    total?: number;
  };
  const rows = parsed.repositories ?? [];
  return `${toolsArray.length} tools listed; scoped personal token's list_indexed_repositories returned ${rows.length} row(s) (total=${parsed.total ?? 0})`;
}

// assertAllowedReadAuditRecorded polls governance_audit_events (bounded, the
// F-9-part-1 async-appender path — see f9-design.md §9 decision 4) for the
// scoped_read_allowed event the tools/call above should have produced, and
// asserts its actor identity is a non-reversible hash, never the raw token.
async function assertAllowedReadAuditRecorded(ctx: ShapeAContext, token: string, sinceIso: string): Promise<string> {
  const row = await pollForGovernanceAuditEvent(
    ctx.repoRoot,
    ctx.project,
    { eventType: "read_authorization", decision: "allowed", reasonCode: "scoped_read_allowed" },
    sinceIso,
    10000,
  );
  if (row.actorClass !== "scoped_token") {
    throw new Error(`expected actor_class=scoped_token, got ${row.actorClass}`);
  }
  if (row.actorIdHash === "") {
    throw new Error("allowed-read audit row has an empty actor_id_hash");
  }
  if (row.actorIdHash.includes(token)) {
    throw new Error("allowed-read audit row's actor_id_hash contains the raw token value");
  }
  return `scoped_read_allowed audit row observed within 10s poll: actor_class=${row.actorClass}, tenant_id=${row.tenantId}, actor_id_hash non-empty and not the raw token`;
}

// assertCredentialLessChallengeIsBare proves a credential-less MCP request on
// a zero-provider deployment gets the pre-#5163 bare "Bearer" challenge, never
// an RFC 9728 resource_metadata directive (PostureOAuthChallengePolicy.OAuthChallenge
// returns ok=false with zero providers).
async function assertCredentialLessChallengeIsBare(ctx: ShapeAContext): Promise<string> {
  const res = await mcpInitialize(ctx.mcpBase, undefined);
  if (res.httpStatus !== 401) {
    throw new Error(`expected 401 from credential-less initialize, got ${res.httpStatus}`);
  }
  if (res.wwwAuthenticate === null) {
    throw new Error("401 response carries no WWW-Authenticate header at all");
  }
  if (res.wwwAuthenticate !== "Bearer") {
    throw new Error(`expected bare "Bearer" challenge with zero providers, got: ${res.wwwAuthenticate}`);
  }
  return `credential-less initialize 401'd with the bare challenge: ${res.wwwAuthenticate}`;
}

// runShapeA runs every shapeA_* step against the given wizard-admin page and
// context, in the order design §4 specifies. adminPage must already be an
// authenticated console session (the post-setup-wizard admin). Returns the
// minted personal token so later shapes (C's "MCP still works via the
// existing personal token" reconfirmation) can reuse it without minting a
// second one.
export async function runShapeA(step: AuthE2EStep, adminPage: Page, ctx: ShapeAContext): Promise<string> {
  await step("shapeA_login_page_token_only_posture", () => assertLoginPageTokenOnlyPosture(ctx));
  await step("shapeA_discovery_404", () => assertDiscoveryRoute404(ctx));

  let personalToken = "";
  await step("shapeA_console_personal_token_created", async () => {
    personalToken = await driveCreatePersonalTokenViaUI(adminPage, "e2e-shapeA-token", ctx.navTimeoutMs);
    return `personal API token minted via /profile's Create-token control (${personalToken.length} chars)`;
  });

  const beforeCallIso = new Date().toISOString();
  await step("shapeA_mcp_tool_call_row_filtered", () => assertMcpToolCallRowFiltered(ctx, personalToken));
  await step("shapeA_allowed_read_audit_recorded", () =>
    assertAllowedReadAuditRecorded(ctx, personalToken, beforeCallIso),
  );
  await step("shapeA_credentialless_challenge_is_bare", () => assertCredentialLessChallengeIsBare(ctx));

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
