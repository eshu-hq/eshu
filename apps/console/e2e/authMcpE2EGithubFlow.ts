// authMcpE2EGithubFlow.ts — shape C (GitHub, stubbed) step bodies for the
// MCP-identity E2E suite (F-9, issue #5170, design §4 "Shape C"). Runs AFTER
// shape A and BEFORE shape B (design §1's load-bearing phase order: a
// GitHub-only provider must NOT enable RFC 9728 discovery — that only
// happens once shape B's OIDC provider is added). Proves the admin
// drawer's GitHub provider CRUD (F-5), that a team-role mapping is REQUIRED
// for login to succeed at all (not merely for permissions — mirrors
// authE2EOidcFlow.ts's createMemberGroupRoleMapping doc comment), the real
// browser OAuth2 round trip against go/cmd/mock-github, and that MCP's
// posture stays token-only with a GitHub-only provider configured.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { Browser, Page } from "playwright";

import { apiFetchInPage, chromiumLaunchArgs as baseChromiumLaunchArgs } from "./authE2EOidcFlow.ts";
import { resolveEshuCommand, type EshuCommand } from "./authE2ECredential.ts";
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import { assertMcpToolCallRowFiltered, type ShapeAContext } from "./authMcpE2ETokenFlow.ts";

const execFileAsync = promisify(execFile);

const navTimeoutMs = 30000;
const mockGithubHostname = "mock-github";

// mockGithubLaunchArgs extends authE2EOidcFlow.ts's chromiumLaunchArgs table
// with the mock-github hostname mapping (docker-compose.e2e.yaml's mock-github
// service comment documents the identical reachability decision the two mock
// OIDC IdPs use). Chromium only accepts ONE --host-resolver-rules flag per
// launch, so this suite's browser launch must build the full combined rules
// string in one place rather than merging two separate flag arrays.
export function chromiumLaunchArgsWithGithub(
  mockOidcPort: string,
  mockOidcAdminPort: string,
  mockGithubPort: string,
): string[] {
  const base = baseChromiumLaunchArgs(mockOidcPort, mockOidcAdminPort)[0]!;
  return [`${base}, MAP ${mockGithubHostname} 127.0.0.1:${mockGithubPort}`];
}

export interface GithubProviderOpts {
  readonly clientId: string;
  readonly clientSecret: string;
  readonly allowedOrgsText: string;
  readonly baseUrl: string;
  readonly apiBaseUrl: string;
}

// driveAddGithubProviderViaUI opens Admin -> Identity & Access -> Providers,
// selects the "GitHub" provider kind, fills GithubProviderFields, runs a test
// sign-in (asserting it passes against the mock-github stub's unauthenticated
// GET / root probe), then saves. Patterned directly on
// authE2EOidcFlow.ts's driveAddOidcProviderViaUI — see that function's doc
// comment for the deterministic-gate reasoning (network response over
// transient DOM, drawer's own React state before Save).
export async function driveAddGithubProviderViaUI(
  page: Page,
  opts: GithubProviderOpts,
): Promise<void> {
  await page.goto("/admin", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".identity-access-panel", { timeout: navTimeoutMs });
  await page.click('button:has-text("Add provider")');
  await page.waitForSelector('[aria-label="Add provider"]', { timeout: navTimeoutMs });

  await page.click('[role="radiogroup"][aria-label="Provider kind"] button:has-text("GitHub")');
  await page.fill('[aria-label="GitHub client ID"]', opts.clientId);
  await page.fill('[aria-label="GitHub client secret"]', opts.clientSecret);
  await page.fill('[aria-label="Allowed organizations"]', opts.allowedOrgsText);
  await page.fill('[aria-label="GitHub base URL"]', opts.baseUrl);
  await page.fill('[aria-label="GitHub API base URL"]', opts.apiBaseUrl);

  let testConnectionResponse: Awaited<ReturnType<Page["waitForResponse"]>>;
  try {
    [testConnectionResponse] = await Promise.all([
      page.waitForResponse(
        (resp) => resp.url().includes("/test-connection") && resp.request().method() === "POST",
        { timeout: navTimeoutMs },
      ),
      page.click('button:has-text("Run test sign-in")'),
    ]);
  } catch (err) {
    throw new Error(
      `test-connection network response never arrived after clicking "Run test sign-in" for the GitHub provider: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
  if (testConnectionResponse.status() !== 200) {
    throw new Error(
      `GitHub provider test-connection returned ${testConnectionResponse.status()} instead of 200`,
    );
  }
  const testConnectionBody: { ok?: boolean } = await testConnectionResponse
    .json()
    .catch(() => ({}) as { ok?: boolean });
  if (testConnectionBody.ok !== true) {
    throw new Error(
      `GitHub provider test-connection responded 200 but body.ok was not true: ${JSON.stringify(testConnectionBody)}`,
    );
  }

  await page.waitForFunction(
    () => {
      const buttons = Array.from(document.querySelectorAll(".drawer button"));
      const saveButton = buttons.find((b) => b.textContent?.trim() === "Save");
      return Boolean(saveButton) && !(saveButton as HTMLButtonElement).disabled;
    },
    undefined,
    { timeout: navTimeoutMs },
  );

  await page.click('.drawer button:has-text("Save")');
  await page.waitForSelector('.drawer p[role="status"]:has-text("Saved and enabled")', {
    timeout: navTimeoutMs,
  });
  await page.click('.drawer button[aria-label="Close"]');
  await page.waitForSelector(".drawer", { state: "detached", timeout: navTimeoutMs });
}

// assertGithubProviderRowActive re-opens /admin's Providers tab and asserts
// the GitHub provider row shows an "active" status badge.
export async function assertGithubProviderRowActive(page: Page, labelText: string): Promise<void> {
  await page.goto("/admin", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector('table[aria-label="Providers"]', { timeout: navTimeoutMs });
  const row = page.locator('table[aria-label="Providers"] tr', { hasText: labelText });
  await row.waitFor({ state: "visible", timeout: navTimeoutMs });
  const activeBadgeCount = await row.getByText("active", { exact: true }).count();
  if (activeBadgeCount === 0) {
    throw new Error(`GitHub provider row for ${labelText} is not showing an "active" status badge`);
  }
}

// findGithubProviderConfigId looks up the provider_config_id of the just-created
// GitHub provider by its base_url, and asserts it is active — mirrors
// authE2EOidcFlow.ts's findProviderConfigIdByIssuer for the OIDC case.
export async function findGithubProviderConfigId(page: Page, baseUrl: string): Promise<string> {
  const result = await apiFetchInPage(page, "GET", "/api/v0/auth/admin/provider-configs");
  if (result.status >= 300) {
    throw new Error(`list provider configs failed (${result.status}): ${result.text}`);
  }
  const parsed: {
    provider_configs?: readonly {
      provider_config_id: string;
      status?: string;
      provider_kind?: string;
      configuration?: { base_url?: string };
    }[];
  } = JSON.parse(result.text || "{}");
  // The admin CRUD read API returns the RAW DB provider_kind
  // ("external_github"), not the short form ("github") the write/form layer
  // uses (verified live: admin_provider_config_build.go's builtProviderConfigWrite.kind
  // is "external_github"; providerConfigDetailJSON copies it verbatim).
  const match = (parsed.provider_configs ?? []).find(
    (item) => item.provider_kind === "external_github" && item.configuration?.base_url === baseUrl,
  );
  if (!match) {
    throw new Error(
      `no github provider config found with base_url ${baseUrl}; provider_configs seen: ${JSON.stringify(parsed.provider_configs)}`,
    );
  }
  if (match.status !== "active") {
    throw new Error(
      `github provider config ${match.provider_config_id} is not active (status=${match.status})`,
    );
  }
  return match.provider_config_id;
}

// createGithubTeamRoleMapping maps a GitHub "org/team-slug" handle to the
// only role_id available in a fresh tenant ("owner") — the same generic
// admin API and constraint authE2EOidcFlow.ts's createMemberGroupRoleMapping
// uses for OIDC groups (identity_provider_group_role_mappings is shared
// across provider kinds).
export async function createGithubTeamRoleMapping(
  page: Page,
  providerConfigId: string,
  teamHandle: string,
): Promise<void> {
  const result = await apiFetchInPage(page, "POST", "/api/v0/auth/admin/idp-group-mappings", {
    provider_config_id: providerConfigId,
    external_group: teamHandle,
    role_id: "owner",
  });
  if (result.status >= 300) {
    throw new Error(
      `create idp group mapping (${teamHandle} -> owner) failed (${result.status}): ${result.text}`,
    );
  }
}

// attemptGithubLoginExpectDenied clicks the GitHub .btn-sso button on /login
// in a fresh context and asserts the callback denies the login with 403
// (go/internal/query/github_login_handler.go's writeGitHubLoginError,
// ErrGitHubLoginDenied) — proving the team-role mapping is REQUIRED for login
// to succeed at all, not merely for permissions (CompleteGitHubLogin returns
// ErrGitHubLoginDenied when zero role grants resolve, mirroring
// CompleteOIDCLogin's identical guard).
export async function attemptGithubLoginExpectDenied(
  browser: Browser,
  baseUrl: string,
): Promise<string> {
  const context = await browser.newContext({ baseURL: baseUrl });
  try {
    const page = await context.newPage();
    await page.goto("/login?tenant_id=default", {
      waitUntil: "domcontentloaded",
      timeout: navTimeoutMs,
    });
    await page.locator(".btn-sso").first().waitFor({ state: "visible", timeout: navTimeoutMs });
    const [callbackResponse] = await Promise.all([
      page.waitForResponse((resp) => resp.url().includes("/github/callback"), {
        timeout: navTimeoutMs,
      }),
      page.locator(".btn-sso").first().click(),
    ]);
    if (callbackResponse.status() !== 403) {
      throw new Error(
        `expected 403 from the GitHub callback with no team-role mapping yet, got ${callbackResponse.status()}`,
      );
    }
    return `GitHub login correctly DENIED (403) before any team-role mapping exists`;
  } finally {
    await context.close();
  }
}

// driveGithubLoginExpectSuccess clicks the GitHub .btn-sso button in a fresh
// context and waits for the authenticated shell to render, mirroring
// authE2EOidcFlow.ts's driveOidcLogin. Returns the opened context so the
// caller can inspect the resulting session's role (admin-nav presence).
export async function driveGithubLoginExpectSuccess(
  browser: Browser,
  baseUrl: string,
): Promise<Awaited<ReturnType<Browser["newContext"]>>> {
  const context = await browser.newContext({ baseURL: baseUrl });
  const page = await context.newPage();
  await page.goto("/login?tenant_id=default", {
    waitUntil: "domcontentloaded",
    timeout: navTimeoutMs,
  });
  await page.locator(".btn-sso").first().waitFor({ state: "visible", timeout: navTimeoutMs });
  // App.tsx's AppSidebar renders UNCONDITIONALLY as soon as the post-login
  // shell mounts, using a fail-open (show-everything) allowedNav set until
  // the async bootFromSession() GET /api/v0/auth/browser-session resolves and
  // re-renders with the real, permission-catalog-enforced set
  // (buildAllowedNavSet's own doc comment: "No session ... -> show
  // everything"). The listener MUST be registered before the click (via
  // Promise.all, not a separate await afterward): a fast mock-github round
  // trip reliably completes that fetch WHILE nav.sidebar is still being
  // detected, so a listener attached only after waitForSelector resolves
  // races the response and can miss it entirely, hanging until timeout
  // (caught live — this exact ordering bug, not a random flake, reproduced
  // deterministically twice before this fix).
  try {
    const [, sessionResponse] = await Promise.all([
      page.locator(".btn-sso").first().click(),
      page.waitForResponse((resp) => resp.url().includes("/auth/browser-session"), {
        timeout: navTimeoutMs,
      }),
      page.waitForSelector("nav.sidebar", { timeout: navTimeoutMs }),
    ]);
    if (!sessionResponse.ok()) {
      throw new Error(`GET .../auth/browser-session returned ${sessionResponse.status()}`);
    }
  } catch (err) {
    await context.close();
    throw new Error(
      `nav.sidebar/browser-session fetch never settled after clicking GitHub .btn-sso (final url: ${page.url()}): ${err instanceof Error ? err.message : String(err)}`,
    );
  }
  return context;
}

// assertDiscoveryStillNotEnabled proves design §4's shape-C posture
// cross-check: a GitHub-only provider must NOT enable RFC 9728 discovery
// (GitHub is never an oidcbearer issuer — auth_oauth_discovery.go's
// zero-active-issuers 404 gate), and the credential-less MCP 401 challenge
// stays bare for the same reason.
async function assertDiscoveryStillNotEnabled(mcpBase: string): Promise<string> {
  const discoveryRes = await fetch(`${mcpBase}/.well-known/oauth-protected-resource`);
  if (discoveryRes.status !== 404) {
    throw new Error(
      `expected discovery to STILL 404 with only a GitHub provider configured, got ${discoveryRes.status}`,
    );
  }
  const initRes = await fetch(`${mcpBase}/mcp/message`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize" }),
  });
  if (initRes.status !== 401) {
    throw new Error(`expected 401 from credential-less initialize, got ${initRes.status}`);
  }
  const challenge = initRes.headers.get("WWW-Authenticate");
  if (challenge !== "Bearer") {
    throw new Error(
      `expected the bare "Bearer" challenge with only a GitHub provider configured, got: ${challenge}`,
    );
  }
  return "discovery route STILL 404s and the credential-less 401 challenge STILL bare with only a GitHub provider configured";
}

export function resolveMcpSetupCommand(
  eshuBinary: string,
  repoGoDir: string,
  mcpBase: string,
): EshuCommand {
  return resolveEshuCommand(eshuBinary, repoGoDir, [
    "mcp",
    "setup",
    "--hosted",
    "--service-url",
    mcpBase,
  ]);
}

// assertMcpSetupPostureIsToken shells out to `eshu mcp setup --hosted
// --service-url <mcpBase>` (design §4 item 6, F-8) and asserts the printed
// snippet references the per-user token env var, not an OAuth-only shape —
// the CLI's own discovery probe against mcpBase should resolve postureToken
// because the discovery route still 404s (see assertDiscoveryStillNotEnabled).
async function assertMcpSetupPostureIsToken(
  repoGoDir: string,
  mcpBase: string,
  eshuBinary = process.env.ESHU_E2E_ESHU_BINARY ?? "",
): Promise<string> {
  const command = resolveMcpSetupCommand(eshuBinary, repoGoDir, mcpBase);
  const { stdout } = await execFileAsync(command.file, command.args, { timeout: 60000 });
  if (!stdout.includes("ESHU_MCP_TOKEN")) {
    throw new Error(
      `expected the printed MCP setup snippet to reference ESHU_MCP_TOKEN (token posture): ${stdout}`,
    );
  }
  return "eshu mcp setup --hosted resolved token posture (snippet references ESHU_MCP_TOKEN)";
}

export interface ShapeCContext extends ShapeAContext {
  readonly repoGoDir: string;
}

// runShapeC runs every shapeC_* step in design §4's order. adminPage must be
// the same authenticated admin session shape A used; personalToken is the
// token shape A already minted (item 5 reconfirms it still works, rather
// than minting a second one).
export async function runShapeC(
  step: AuthE2EStep,
  adminPage: Page,
  ctx: ShapeCContext,
  personalToken: string,
): Promise<void> {
  const githubBaseUrl = "http://mock-github:8080";
  const org = "eshu-e2e-org";
  const team = "platform-team";
  const teamHandle = `${org}/${team}`;

  await step("shapeC_admin_drawer_provider_crud", async () => {
    await driveAddGithubProviderViaUI(adminPage, {
      clientId: "eshu-e2e-github-client",
      clientSecret: "unused-github-provider-secret-e2e",
      allowedOrgsText: org,
      baseUrl: githubBaseUrl,
      apiBaseUrl: githubBaseUrl,
    });
    await assertGithubProviderRowActive(adminPage, githubBaseUrl);
    return `GitHub provider tested, saved, and shows active (base_url=${githubBaseUrl})`;
  });

  let providerConfigId = "";
  await step("shapeC_login_denied_before_team_mapping", async () => {
    providerConfigId = await findGithubProviderConfigId(adminPage, githubBaseUrl);
    return await attemptGithubLoginExpectDenied(ctx.browser, ctx.baseUrl);
  });

  await step("shapeC_team_role_mapping_created", async () => {
    await createGithubTeamRoleMapping(adminPage, providerConfigId, teamHandle);
    return `mapped team "${teamHandle}" on provider ${providerConfigId} to role_id "owner"`;
  });

  let memberContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
  await step("shapeC_login_succeeds_after_mapping", async () => {
    memberContext = await driveGithubLoginExpectSuccess(ctx.browser, ctx.baseUrl);
    return "GitHub identity completed OAuth2 round trip against mock-github and reached the authenticated shell";
  });

  await step("shapeC_member_session_role_from_mapping", async () => {
    const memberPage = memberContext!.pages()[0]!;
    // Bounded poll, not a single point-in-time check: the browser-session GET
    // resolving (already awaited by driveGithubLoginExpectSuccess) does not
    // guarantee React has committed the resulting allowedNav re-render on the
    // very next Playwright round trip — caught live as a residual, low-rate
    // flake (1/3 runs) even after fixing the listener-registration race.
    // React's commit is a same-tick synchronous update once bootFromSession's
    // .then() resolves, so this settles within a few polls, not a fixed sleep.
    try {
      await memberPage.waitForFunction(
        () => document.querySelectorAll('nav.sidebar a[aria-label="Admin"]').length === 0,
        undefined,
        { timeout: 5000, polling: 100 },
      );
    } catch {
      const sessionRes = await apiFetchInPage(memberPage, "GET", "/api/v0/auth/browser-session");
      throw new Error(
        `GitHub-mapped "owner"-role session still renders an Admin nav link after a 5s settle poll. browser-session (${sessionRes.status}): ${sessionRes.text}`,
      );
    }
    return "no Admin nav link rendered (settled within the poll window) — the DB-backed team->role mapping produced a non-admin, permission-catalog-enforced session";
  });
  if (memberContext) await memberContext.close();

  await step("shapeC_discovery_and_challenge_unaffected", () =>
    assertDiscoveryStillNotEnabled(ctx.mcpBase),
  );

  await step("shapeC_mcp_still_works_via_personal_token", () =>
    assertMcpToolCallRowFiltered(ctx, personalToken),
  );

  await step("shapeC_mcp_setup_posture_is_token", () =>
    assertMcpSetupPostureIsToken(ctx.repoGoDir, ctx.mcpBase),
  );
}
