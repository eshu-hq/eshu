// authMcpE2EShapeB.ts — shape B (Okta-style OIDC via the mock IdP) step
// bodies for the MCP-identity E2E suite (F-9, issue #5170, design §4 "Shape
// B"). Runs LAST (after shape A and shape C — design §1's load-bearing
// order): this is the only shape that flips RFC 9728 discovery live and ends
// with the require_sso guardrail, which revokes local sessions and must not
// precede shape C's browser work.
import type { Browser, Page } from "playwright";

import {
  apiFetchInPage,
  createMemberGroupRoleMapping,
  driveOidcLogin,
  driveAddOidcProviderViaUI,
  findProviderConfigIdByIssuer,
} from "./authE2EOidcFlow.ts";
import {
  assertBreakglassLocalLoginStillWorks,
  assertLoginHidesLocalForm,
  completeAdminSSOLoginPrecondition,
  enableRequireSSO,
} from "./authE2ERequireSSOFlow.ts";
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import { mcpInitialize, mcpToolsList } from "./authMcpE2EJsonRpc.ts";
import {
  assertScriptedTokenWorksAgainstMcp,
  mintJwtFromMockIdp,
  rewriteInNetworkHost,
  runScriptedOAuthClient,
  type HostRewriteTable,
} from "./authMcpE2EOauthClient.ts";
import type { ShapeCContext } from "./authMcpE2EGithubFlow.ts";

const navTimeoutMs = 30000;
const memberOidcIssuer = "http://mock-oidc-idp:8080";
const memberOidcGroup = "member"; // matches docker-compose.e2e.yaml's mock-oidc-idp MOCK_OIDC_GROUPS default
const adminStaticProviderId = "pc_e2e_admin_static";

export interface ShapeBContext extends ShapeCContext {
  readonly hostRewrite: HostRewriteTable;
  readonly screenshotsDir: string;
  // Wizard-admin credentials, threaded from the bootstrap step, needed by
  // assertBreakglassLocalLoginStillWorks.
  readonly credentialUsername: string;
  readonly wizardNewPassword: string;
  readonly breakglassRecoveryCode: string;
}

// pollForDiscoveryEnabled polls GET /.well-known/oauth-protected-resource
// until it returns 200 or deadlineMs elapses (design §4 shape B item 3: the
// oidcbearer issuer snapshot has a fixed 30s TTL — go/internal/oidcbearer/
// resolver.go's defaultTTL — with no configurable knob, so this bounds a
// real poll rather than relying on one).
async function pollForDiscoveryEnabled(
  mcpBase: string,
  deadlineMs: number,
): Promise<Record<string, unknown>> {
  const start = Date.now();
  let lastStatus = 0;
  while (Date.now() - start < deadlineMs) {
    const res = await fetch(`${mcpBase}/.well-known/oauth-protected-resource`);
    lastStatus = res.status;
    if (res.status === 200) {
      return (await res.json()) as Record<string, unknown>;
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`discovery never flipped to 200 within ${deadlineMs}ms (last status ${lastStatus})`);
}

// runShapeB returns the OAuth-minted bearer access token (member group ->
// "owner" role -> empty repository grant, so AllScopes=false), which the
// negative-leakage module reuses as its SCOPED credential for the non-vacuous
// row-filter proof (design §5). It also returns the SSO-admin browser context
// (external_oidc_user, AllScopes) LEFT OPEN: that session SURVIVES the
// require_sso flip (unlike the wizard-admin local session, which the flip
// revokes), so it is the only live AllScopes reader the leakage module can use
// for the "AllScopes sees the seeded repo" half of the row-filter proof. The
// runner closes it after the leakage module finishes.
export interface ShapeBResult {
  readonly scopedBearer: string;
  readonly ssoAdminContext: Awaited<ReturnType<Browser["newContext"]>>;
}

export async function runShapeB(
  step: AuthE2EStep,
  adminPage: Page,
  ctx: ShapeBContext,
  personalToken: string,
): Promise<ShapeBResult> {
  await step("shapeB_admin_drawer_provider_crud", async () => {
    await driveAddOidcProviderViaUI(adminPage, {
      issuer: memberOidcIssuer,
      clientId: "eshu-e2e-member",
      clientSecret: "unused-member-provider-secret-e2e",
      scopesText: "openid, profile, email, groups",
      groupClaim: "groups",
    });
    return `OIDC provider for issuer ${memberOidcIssuer} tested and saved`;
  });

  let memberProviderConfigId = "";
  await step("shapeB_member_group_mapped_to_owner_role", async () => {
    memberProviderConfigId = await findProviderConfigIdByIssuer(adminPage, memberOidcIssuer);
    await createMemberGroupRoleMapping(adminPage, memberProviderConfigId, memberOidcGroup);
    return `mapped group "${memberOidcGroup}" on provider ${memberProviderConfigId} to role_id "owner"`;
  });

  await step("shapeB_login_page_shows_provider_button", async () => {
    const loginContext = await ctx.browser.newContext({ baseURL: ctx.baseUrl });
    try {
      const loginPage = await loginContext.newPage();
      await loginPage.goto("/login?tenant_id=default", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
      await loginPage.waitForSelector(".btn-sso", { timeout: navTimeoutMs });
      const count = await loginPage.locator(".btn-sso").count();
      if (count < 1) {
        throw new Error("no SSO button rendered on /login after enabling the member OIDC provider");
      }
      return `${count} SSO button(s) rendered on /login`;
    } finally {
      await loginContext.close();
    }
  });

  let memberContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
  await step("shapeB_browser_login_succeeds", async () => {
    memberContext = await ctx.browser.newContext({ baseURL: ctx.baseUrl });
    const memberPage = await memberContext.newPage();
    await driveOidcLogin(memberPage);
    return "member identity completed OIDC redirect -> mock IdP -> callback -> dashboard";
  });
  if (memberContext) await memberContext.close();

  let discoveryDoc: Record<string, unknown> = {};
  await step("shapeB_discovery_flips_live", async () => {
    discoveryDoc = await pollForDiscoveryEnabled(ctx.mcpBase, 60000);
    const resource = discoveryDoc.resource;
    const servers = discoveryDoc.authorization_servers as readonly string[] | undefined;
    const bearerMethods = discoveryDoc.bearer_methods_supported as readonly string[] | undefined;
    const scopes = discoveryDoc.scopes_supported as readonly string[] | undefined;
    if (resource !== ctx.mcpBase) {
      throw new Error(`discovery resource = ${String(resource)}, want ${ctx.mcpBase}`);
    }
    if (!servers || servers.length !== 1 || servers[0] !== memberOidcIssuer) {
      throw new Error(`discovery authorization_servers = ${JSON.stringify(servers)}, want [${memberOidcIssuer}]`);
    }
    if (!bearerMethods || bearerMethods.length !== 1 || bearerMethods[0] !== "header") {
      throw new Error(`discovery bearer_methods_supported = ${JSON.stringify(bearerMethods)}, want [header]`);
    }
    if (!scopes || !scopes.includes("groups")) {
      throw new Error(`discovery scopes_supported = ${JSON.stringify(scopes)}, missing "groups"`);
    }
    return `discovery flipped to 200 with resource=${String(resource)}, authorization_servers=[${memberOidcIssuer}]`;
  });

  let oauthAccessToken = "";
  await step("shapeB_scripted_oauth_chain_tools_call", async () => {
    // ESHU_AUTH_RESOURCE_URI (docker-compose.e2e.yaml's mcp-server service) is
    // set to exactly this same host-reachable mcpBase value, so it is both
    // the expected metadata "resource" AND the resource indicator this
    // client requests.
    const result = await runScriptedOAuthClient(ctx.mcpBase, ctx.mcpBase, ctx.hostRewrite);
    oauthAccessToken = result.accessToken;
    const proof = await assertScriptedTokenWorksAgainstMcp(ctx.mcpBase, oauthAccessToken);
    return `scripted RFC 9728 OAuth chain (401 -> metadata -> discovery -> authorize+PKCE -> token) minted a JWT; ${proof}`;
  });

  await step("shapeB_precedence_personal_token_no_challenge_header", async () => {
    const res = await mcpInitialize(ctx.mcpBase, personalToken);
    if (res.httpStatus !== 200) {
      throw new Error(`shape A's personal token expected 200 with OIDC active, got ${res.httpStatus}: ${res.bodyText}`);
    }
    if (res.wwwAuthenticate !== null) {
      throw new Error(
        `expected NO WWW-Authenticate header on a 200 response, got: ${res.wwwAuthenticate}`,
      );
    }
    return "personal token still 200s with the OIDC provider active, and carries no WWW-Authenticate header at all";
  });

  await step("shapeB_precedence_wrong_audience_bare_challenge", async () => {
    const wrongAudienceToken = await mintJwtFromMockIdp(
      rewriteInNetworkHost(memberOidcIssuer, ctx.hostRewrite),
      "https://wrong.example",
    );
    const res = await mcpInitialize(ctx.mcpBase, wrongAudienceToken);
    if (res.httpStatus !== 401) {
      throw new Error(`wrong-audience JWT expected 401, got ${res.httpStatus}`);
    }
    if (res.wwwAuthenticate !== "Bearer") {
      throw new Error(
        `expected the bare "Bearer" challenge for a post-match (wrong-audience) denial, got: ${res.wwwAuthenticate}`,
      );
    }
    return "wrong-audience JWT 401'd with the bare challenge (post-match denial never steers to discovery)";
  });

  await step("shapeB_precedence_unknown_issuer_resource_metadata_challenge", async () => {
    // mock-oidc-idp-admin is never registered as a bearer-active issuer for
    // this tenant (only the member-mapped mock-oidc-idp is) — a token it
    // signs is pre-match "not a recognized issued token"
    // (ErrBearerCredentialUnrecognized), which DOES steer to discovery.
    const mockOidcAdminBase = rewriteInNetworkHost("http://mock-oidc-idp-admin:8080", ctx.hostRewrite);
    const unknownIssuerToken = await mintJwtFromMockIdp(mockOidcAdminBase, String(discoveryDoc.resource));
    const res = await mcpInitialize(ctx.mcpBase, unknownIssuerToken);
    if (res.httpStatus !== 401) {
      throw new Error(`unknown-issuer JWT expected 401, got ${res.httpStatus}`);
    }
    if (res.wwwAuthenticate === null || !res.wwwAuthenticate.includes("resource_metadata=")) {
      throw new Error(
        `expected the resource_metadata challenge for a pre-match (unknown-issuer) denial, got: ${res.wwwAuthenticate}`,
      );
    }
    return `unknown-issuer JWT 401'd with a resource_metadata challenge: ${res.wwwAuthenticate}`;
  });

  let ssoAdminContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
  await step("shapeB_require_sso_admin_precondition", async () => {
    const { context, detail } = await completeAdminSSOLoginPrecondition(
      ctx.browser,
      ctx.baseUrl,
      adminStaticProviderId,
    );
    ssoAdminContext = context;
    return detail;
  });

  await step("shapeB_require_sso_enabled", () => enableRequireSSO(ssoAdminContext!.pages()[0]!));

  await step("shapeB_require_sso_login_hides_local_form", () =>
    assertLoginHidesLocalForm(ctx.browser, ctx.baseUrl, navTimeoutMs, ctx.screenshotsDir),
  );

  await step("shapeB_require_sso_breakglass_still_works", () =>
    assertBreakglassLocalLoginStillWorks(
      ctx.browser,
      ctx.baseUrl,
      navTimeoutMs,
      ctx.screenshotsDir,
      ctx.credentialUsername,
      ctx.wizardNewPassword,
      ctx.breakglassRecoveryCode,
    ),
  );

  await step("shapeB_require_sso_personal_token_still_works", async () => {
    const res = await mcpToolsList(ctx.mcpBase, personalToken);
    if (res.httpStatus !== 200) {
      throw new Error(`personal token expected 200 after require_sso flip, got ${res.httpStatus}: ${res.bodyText}`);
    }
    return "personal token still authenticates against MCP after require_sso=true (tokens are not sessions)";
  });

  await step("shapeB_require_sso_oauth_bearer_still_works", async () => {
    const res = await mcpToolsList(ctx.mcpBase, oauthAccessToken);
    if (res.httpStatus !== 200) {
      throw new Error(`OAuth bearer expected 200 after require_sso flip, got ${res.httpStatus}: ${res.bodyText}`);
    }
    return "OAuth-minted bearer token still authenticates against MCP after require_sso=true";
  });

  await step("shapeB_sso_admin_session_survives_flip", async () => {
    // Final sanity read proving the SSO-admin session that performed the flip
    // is itself still usable. The context is NOT closed here — the leakage
    // module reuses it as the AllScopes reader (see ShapeBResult); the runner
    // closes it after leakage finishes.
    const result = await apiFetchInPage(ssoAdminContext!.pages()[0]!, "GET", "/api/v0/auth/admin/sign-in-policy");
    if (result.status !== 200) {
      throw new Error(`SSO-admin session read after the flip expected 200, got ${result.status}`);
    }
    return "SSO-admin session remained valid through the flip it performed";
  });

  return { scopedBearer: oauthAccessToken, ssoAdminContext: ssoAdminContext! };
}
