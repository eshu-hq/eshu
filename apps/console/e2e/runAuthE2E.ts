// Browser-auth E2E runner (issue #4971 phase 2, epic #4962 closer).
//
// Drives a REAL browser through the console's first-run setup wizard and the
// require_sso guardrail against a freshly booted, zero-corpus
// docker-compose.e2e.yaml stack (see that file and
// docs/public/run-locally/docker-compose.md#sso-auth-e2e-stack). Unlike
// e2e/runConsoleLiveE2E.ts (which authenticates with a pre-shared API key
// against a corpus-bearing stack), this runner authenticates the way a real
// operator does on first boot: no local identities exist yet, so it recovers
// the generated one-time admin credential straight out of Postgres via the
// `eshu` CLI (authE2ECredential.ts) and drives the browser session that
// results from claiming it.
//
// Stack lifecycle: this runner does NOT start or stop the Compose stack.
// scripts/run-auth-e2e.sh owns `docker compose -f docker-compose.e2e.yaml up
// --wait` before invoking this runner and `down -v` after, mirroring
// scripts/run-console-live-e2e.sh's "the gate does not manage Docker"
// convention but going one step further: because this stack must start from
// truly zero identities for the acceptance items to mean anything, the
// wrapper always brings up a FRESH stack (never reuses a long-lived one) and
// always tears it down, rather than leaving stack lifecycle "explicit and
// operator-controlled" the way the live corpus gate does.
//
// Console-reachability decision: see authE2EDevServer.ts's header comment.
//
// Acceptance coverage (issue #4971 phase 2 scope):
//   1. Fresh stack, zero identities -> console shows SetupPage, not a
//      dead-end LoginPage.
//   2. Claim the generated credential, complete the 3-step wizard, land on
//      the dashboard; the bootstrap credential is destroyed (a second
//      retrieval returns nothing) and the setup routes now 410.
//   5. require_sso enforcement. Only the guardrail-rejection half is
//      provable without an OIDC provider: go/internal/query/
//      sign_in_policy_mutations.go's handleUpdate rejects require_sso=true
//      with 400 unless the tenant has (a) a provider config with a passing
//      connection test AND (b) at least one admin who has completed an SSO
//      sign-in (go/internal/storage/postgres/identity_sign_in_policy.go).
//      Neither precondition is reachable in phase 2 (items 3/4 — OIDC
//      provider add/test/enable and browser-reachable mock-IdP wiring — are
//      explicitly phase 3). This runner proves the guardrail itself works
//      (a real, meaningful assertion: an admin cannot lock the tenant into
//      SSO-only before SSO is proven to work) and leaves the "require_sso
//      actually enabled -> local form hidden -> break-glass ?local=1 still
//      works" assertion as an explicit phase-3 TODO below, rather than
//      seeding the guardrail's preconditions directly in Postgres to force a
//      false pass — that would prove the login page's conditional rendering
//      but nothing about the actual SSO-gating feature under test.
//
// Phase 3 (this revision) adds items 3, 4, and the item-5 break-glass half:
//   3. Configure a member-mapped OIDC provider through the real Add-provider
//      UI, run its test sign-in, save/enable it, and confirm it shows active
//      and appears on the login page.
//   4. Complete a real browser OIDC redirect -> mock IdP -> callback login as
//      that member-mapped, non-admin identity; assert no Admin nav, a 403
//      AccessDeniedPage on direct /admin navigation, and a 403 from an admin
//      API call under that session.
//   5. (completing the guardrail) after item 3's provider has a passing test
//      AND a second, admin-mapped SSO login has proven the guardrail's other
//      precondition (see authE2EOidcFlow.ts's header comment for why that
//      needs a second, env/file-backed provider — a DB-backed group mapping
//      can never produce an AllScopes session), enable require_sso and
//      assert the local form disappears on /login and reappears (with local
//      admin sign-in still working) on /login?local=1.
//
// Item 6 (negative-leakage scan) and the CI job remain phase 4 scope.
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";

import { buildPostgresDSN, e2eDefaultAuthSecretEncKey, retrieveInitialCredential } from "./authE2ECredential.ts";
import { startAuthE2EDevServer, stopAuthE2EDevServer, type AuthE2EDevServer } from "./authE2EDevServer.ts";
import {
  apiFetchInPage,
  assertProviderRowActive,
  chromiumLaunchArgs,
  createMemberGroupRoleMapping,
  driveAddOidcProviderViaUI,
  driveDirectOidcLogin,
  driveLocalLogin,
  driveOidcLogin,
  findProviderConfigIdByIssuer,
} from "./authE2EOidcFlow.ts";

const here = dirname(fileURLToPath(import.meta.url));
const consoleDir = resolve(here, "..");
const repoRoot = resolve(consoleDir, "..", "..");
const repoGoDir = resolve(repoRoot, "go");
const artifactsDir = resolve(repoRoot, "e2e-artifacts");
const screenshotsDir = resolve(artifactsDir, "auth-e2e-screenshots");
const reportPath = resolve(artifactsDir, "auth-e2e-report.json");

const apiBase = (process.env.ESHU_E2E_API_BASE ?? "http://127.0.0.1:28080").trim();
const postgresDSN =
  (process.env.ESHU_E2E_POSTGRES_DSN ?? "").trim() ||
  buildPostgresDSN({
    host: (process.env.ESHU_E2E_POSTGRES_HOST ?? "127.0.0.1").trim(),
    port: (process.env.ESHU_E2E_POSTGRES_PORT ?? "28432").trim(),
    password: (process.env.ESHU_E2E_POSTGRES_PASSWORD ?? "change-me").trim(),
    database: "eshu",
  });
const authSecretEncKey = (process.env.ESHU_AUTH_SECRET_ENC_KEY ?? "").trim() || e2eDefaultAuthSecretEncKey;
// A distinct port from both the live corpus gate (5180) and the mock-mode
// per-page harness (5190) so all three can run concurrently on one machine.
// Fixed (not dynamically discovered): apps/console/e2e/fixtures/
// oidc-static-config.json hardcodes this exact port in its redirect_url, so
// changing it here requires updating that file too (see its provider's
// redirect_url comment and docker-compose.e2e.yaml's ESHU_AUTH_OIDC_CONFIG_FILE
// comment for why the redirect URI must be a fixed, absolute, host-reachable
// URL rather than a runtime-discovered one).
const devServerPort = 5185;
const navTimeoutMs = 30000;
const mockOidcPort = (process.env.ESHU_E2E_MOCK_OIDC_PORT ?? "28090").trim();
const mockOidcAdminPort = (process.env.ESHU_E2E_MOCK_OIDC_ADMIN_PORT ?? "28091").trim();
// wizardNewPassword is the replacement password driveSetupWizard always
// submits when the setup wizard forces a password change. Item 5's
// break-glass local-login assertion reuses this exact value (the one-time
// bootstrap password retrieveInitialCredential returns is consumed by the
// wizard and no longer valid after setup completes).
const wizardNewPassword = "E2E-auth-runner-P@ssw0rd-1";

interface StepResult {
  readonly id: string;
  readonly status: "pass" | "fail" | "blocked";
  readonly detail: string;
  readonly ms: number;
}

const results: StepResult[] = [];

async function step(id: string, fn: () => Promise<string>): Promise<void> {
  const start = Date.now();
  try {
    const detail = await fn();
    results.push({ id, status: "pass", detail, ms: Date.now() - start });
    process.stdout.write(`  PASS ${id} (${Date.now() - start}ms): ${detail}\n`);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    results.push({ id, status: "fail", detail, ms: Date.now() - start });
    process.stdout.write(`  FAIL ${id} (${Date.now() - start}ms): ${detail}\n`);
  }
}

// patchSignInPolicy drives PATCH /api/v0/auth/admin/sign-in-policy from
// inside the page's own JS context via apiFetchInPage (authE2EOidcFlow.ts),
// reusing the real browser session cookie and CSRF token the current page
// holds.
const patchSignInPolicy = async (
  page: Page,
  body: Record<string, unknown>,
): Promise<{ status: number; text: string }> =>
  apiFetchInPage(page, "PATCH", "/api/v0/auth/admin/sign-in-policy", body);

async function assertFreshStackShowsSetupWizard(page: Page, baseUrl: string): Promise<void> {
  // apiBaseUrl is the dev server's OWN absolute origin + "/eshu-api", not the
  // bare relative "/eshu-api/" phase 2 used. Every fetch this makes is still
  // same-origin (the page IS served from this origin), so nothing about
  // normal API calls changes — but oidcRedirectUri()/samlAcsUrl()
  // (apps/console/src/api/adminProviderConfig.ts) derive the OIDC/SAML
  // redirect URIs registered with a provider directly from this value, and
  // those values are sent to real external IdPs and become literal
  // `Location:` redirect targets a real browser navigates to. A relative
  // "/eshu-api/..." redirect_uri would resolve against the IdP's OWN origin
  // once the mock IdP 302s the browser there (url.Parse+http.Redirect in
  // go/cmd/mock-oidc-idp/server.go accepts it verbatim, same as any real
  // OIDC/SAML provider), landing on a nonsense path on the wrong host. Making
  // this absolute is what item 3/4/5's real browser OIDC round trip needs;
  // item 1/2 (local-only, no external redirect) are unaffected.
  await page.addInitScript(
    ([base]: readonly string[]) => {
      window.localStorage.setItem(
        "eshu.console.environment",
        JSON.stringify({ mode: "private", apiBaseUrl: base, recentApiBaseUrls: [base] }),
      );
    },
    [`${baseUrl}/eshu-api`],
  );
  await page.goto(baseUrl, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".setup-card", { timeout: navTimeoutMs });
  const loginFieldCount = await page.locator("#login-id").count();
  if (loginFieldCount !== 0) {
    throw new Error("LoginPage's #login-id field is present alongside SetupPage — dead-end login form");
  }
}

async function driveSetupWizard(page: Page, username: string, password: string): Promise<void> {
  await page.fill("#setup-username", username);
  await page.fill("#setup-password", password);
  await page.click('.card-foot button[type="submit"]');

  await page.waitForSelector("#setup-new-password", { timeout: navTimeoutMs });
  await page.fill("#setup-new-password", wizardNewPassword);
  await page.fill("#setup-confirm-password", wizardNewPassword);
  await page.click('.card-foot button[type="submit"]');

  // Click the <label> rather than the checkbox input directly.
  // authFlow.css visually hides the native input (position: absolute;
  // opacity: 0; width/height: 0) behind a styled `.checkbox` sibling span
  // (the standard accessible-custom-checkbox pattern), so it has no visible,
  // in-viewport bounding box for Playwright to click — even with
  // force:true. The wrapping <label htmlFor="setup-codes-saved"> has real
  // dimensions and toggles the input via the native label/input
  // association, exactly like a real user clicking the visible row.
  await page.waitForSelector("#setup-codes-saved", { timeout: navTimeoutMs, state: "attached" });
  await page.click('label[for="setup-codes-saved"]');
  await page.click('button:has-text("Finish setup")');

  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
}

async function assertSetupRouteGone(): Promise<void> {
  const res = await fetch(`${apiBase}/api/v0/auth/setup/claim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: "irrelevant", password: "irrelevant" }),
  });
  if (res.status !== 410) {
    throw new Error(`expected 410 Gone from POST /api/v0/auth/setup/claim after setup, got ${res.status}`);
  }
}

// memberOidcIssuer/adminStaticProviderId/memberOidcGroup identify the two
// mock IdPs and providers in play — see authE2EOidcFlow.ts's header comment
// for why item 5's guardrail precondition needs a SECOND, env/file-backed
// provider distinct from the member-mapped one item 3 configures through the
// UI, and why that second provider is driven by direct URL navigation rather
// than an on-page button.
const memberOidcIssuer = "http://mock-oidc-idp:8080";
const memberOidcGroup = "member"; // matches docker-compose.e2e.yaml's mock-oidc-idp MOCK_OIDC_GROUPS default
const adminStaticProviderId = "pc_e2e_admin_static";

export async function runAuthE2E(): Promise<number> {
  const runStart = Date.now();
  await rm(screenshotsDir, { recursive: true, force: true });
  await rm(reportPath, { force: true });
  await mkdir(screenshotsDir, { recursive: true });

  process.stdout.write(`auth-e2e: api base ${apiBase}\n`);

  let devServer: AuthE2EDevServer | undefined;
  let browser: Browser | undefined;
  try {
    devServer = await startAuthE2EDevServer(repoRoot, apiBase, devServerPort);
    // --host-resolver-rules lets this one Chromium process resolve the two
    // Compose-network-only mock IdP hostnames to their host-published ports —
    // see docker-compose.e2e.yaml's mock-oidc-idp comment and
    // authE2EOidcFlow.ts's chromiumLaunchArgs doc comment for the full
    // reachability decision.
    browser = await chromium.launch({ args: chromiumLaunchArgs(mockOidcPort, mockOidcAdminPort) });
    // baseURL lets every relative page.goto("/admin"), page.goto("/login"),
    // etc. below (this context's and every fresh context items 4/5 open)
    // resolve against the dev server's own origin without repeating it.
    const context = await browser.newContext({ baseURL: devServer.baseUrl });
    const page = await context.newPage();

    await step("item1_setup_wizard_renders", async () => {
      await assertFreshStackShowsSetupWizard(page, devServer!.baseUrl);
      await page.screenshot({ path: resolve(screenshotsDir, "1-setup-wizard.png"), fullPage: true });
      return "SetupPage rendered on first navigation; no #login-id field present";
    });

    let credentialUsername = "";
    // The setup wizard forces a password change (driveSetupWizard fills
    // #setup-new-password/#setup-confirm-password), so the credential this
    // captures is the ONE-TIME bootstrap password — item 5's break-glass
    // local-login assertion below uses the wizard's chosen replacement
    // instead (module-level wizardNewPassword).
    await step("item2_retrieve_initial_credential", async () => {
      const { credential, rawStderr } = await retrieveInitialCredential(repoGoDir, postgresDSN, authSecretEncKey);
      if (!credential) {
        throw new Error(`eshu admin initial-credential returned nothing on a fresh stack: ${rawStderr}`);
      }
      credentialUsername = credential.username;
      await driveSetupWizard(page, credential.username, credential.password);
      await page.screenshot({ path: resolve(screenshotsDir, "2-dashboard.png"), fullPage: true });
      return `wizard completed as ${credential.username}; dashboard reached (.source-pill.src-connected)`;
    });

    await step("item2_credential_consumed", async () => {
      const { credential, rawStderr } = await retrieveInitialCredential(repoGoDir, postgresDSN, authSecretEncKey);
      if (credential) {
        throw new Error("eshu admin initial-credential still returns a value after setup completed — not consumed");
      }
      return `second retrieval correctly empty: ${rawStderr.trim().slice(0, 160)}`;
    });

    await step("item2_setup_routes_gone", async () => {
      await assertSetupRouteGone();
      return "POST /api/v0/auth/setup/claim now returns 410 Gone";
    });

    await step("item5_guardrail_rejects_premature_enable", async () => {
      const result = await patchSignInPolicy(page, { require_sso: true });
      if (result.status !== 400) {
        throw new Error(
          `expected 400 from the require_sso guardrail with no proven provider/SSO admin, got ${result.status}: ${result.text}`,
        );
      }
      if (!result.text.includes("require_sso cannot be enabled")) {
        throw new Error(`400 body did not carry the expected guardrail message: ${result.text}`);
      }
      return `PATCH require_sso=true correctly rejected (400): ${result.text}`;
    });

    // item 3: configure the member-mapped OIDC provider through the real
    // Add-provider UI, on the ORIGINAL admin `page` (still the wizard
    // admin's local session).
    await step("item3_configure_member_oidc_provider", async () => {
      await driveAddOidcProviderViaUI(page, {
        issuer: memberOidcIssuer,
        clientId: "eshu-e2e-member",
        clientSecret: "unused-member-provider-secret-e2e",
        scopesText: "openid, profile, email, groups",
        groupClaim: "groups",
      });
      await assertProviderRowActive(page, memberOidcIssuer);
      await page.screenshot({ path: resolve(screenshotsDir, "3-provider-active.png"), fullPage: true });
      return `provider for issuer ${memberOidcIssuer} tested, saved, and shows active`;
    });

    // The mock IdP's identity carries group "member" (docker-compose.e2e.yaml
    // default). Without a matching identity_provider_group_role_mappings row,
    // CompleteOIDCLogin (go/internal/oidclogin/service.go) denies the login
    // outright (len(grants.RoleIDs)==0) — this mapping is required for item
    // 4's login to succeed at all, not merely to shape its permissions. See
    // createMemberGroupRoleMapping's doc comment for why role_id "owner" (the
    // only role that exists in a fresh tenant) still produces a genuinely
    // unprivileged session over the DB-backed OIDC grant path.
    let memberProviderConfigId = "";
    await step("item3_member_group_mapped_to_owner_role", async () => {
      memberProviderConfigId = await findProviderConfigIdByIssuer(page, memberOidcIssuer);
      await createMemberGroupRoleMapping(page, memberProviderConfigId, memberOidcGroup);
      return `mapped group "${memberOidcGroup}" on provider ${memberProviderConfigId} to role_id "owner"`;
    });

    await step("item3_provider_button_on_login_page", async () => {
      const loginContext = await browser!.newContext({ baseURL: devServer!.baseUrl });
      const loginPage = await loginContext.newPage();
      try {
        await loginPage.goto("/login", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
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

    // item 4: complete a real browser OIDC login as the member-mapped,
    // non-admin identity, in a FRESH context so it starts from zero
    // identities (never reuses the wizard admin's cookies).
    let memberContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
    await step("item4_member_sso_login_completes", async () => {
      memberContext = await browser!.newContext({ baseURL: devServer!.baseUrl });
      const memberPage = await memberContext.newPage();
      await driveOidcLogin(memberPage);
      await memberPage.screenshot({ path: resolve(screenshotsDir, "4-member-dashboard.png"), fullPage: true });
      return "member identity completed OIDC redirect -> mock IdP -> callback -> dashboard";
    });

    await step("item4_member_has_no_admin_nav", async () => {
      const memberPage = memberContext!.pages()[0]!;
      const adminLinkCount = await memberPage.locator('nav.sidebar a[aria-label="Admin"]').count();
      if (adminLinkCount !== 0) {
        throw new Error("member session's sidebar unexpectedly renders an Admin nav link");
      }
      return "no Admin nav link rendered for the member session";
    });

    await step("item4_member_admin_route_shows_access_denied", async () => {
      const memberPage = memberContext!.pages()[0]!;
      await memberPage.goto("/admin", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
      await memberPage.waitForSelector(".access-denied-panel", { timeout: navTimeoutMs });
      const adminShellCount = await memberPage.locator(".panel-grid").count();
      if (adminShellCount !== 0) {
        throw new Error("member session's /admin navigation rendered the admin shell, not AccessDeniedPage");
      }
      return "GET /admin rendered AccessDeniedPage (403 screen), not the admin shell";
    });

    await step("item4_member_admin_api_call_403s", async () => {
      const memberPage = memberContext!.pages()[0]!;
      const result = await apiFetchInPage(memberPage, "GET", "/api/v0/auth/admin/provider-configs");
      if (result.status !== 403) {
        throw new Error(`expected 403 from an admin API call under the member session, got ${result.status}`);
      }
      return `GET /api/v0/auth/admin/provider-configs correctly returned 403: ${result.text.slice(0, 160)}`;
    });

    // item 5 (completing the break-glass half): complete an SSO login
    // through the env/file-backed admin provider in a fresh context — no DB
    // row is created for it (see authE2EOidcFlow.ts's header comment: the
    // admin mutation API refuses to manage an env-shadowed provider_config_id
    // at all, verified live via the 400 "managed by environment" response).
    // This identity resolves to an AllScopes=true session purely through
    // ESHU_AUTH_OIDC_CONFIG_FILE's role_grants (docker-compose.e2e.yaml),
    // recording the guardrail's SSO-admin-proof precondition.
    let ssoAdminContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
    await step("item5_precondition_admin_sso_login", async () => {
      ssoAdminContext = await browser!.newContext({ baseURL: devServer!.baseUrl });
      const ssoAdminPage = await ssoAdminContext.newPage();
      await driveDirectOidcLogin(ssoAdminPage, adminStaticProviderId);
      return "admin-mapped identity completed SSO login, recording sso_admin_verified_at for the tenant";
    });

    await step("item5_enable_require_sso", async () => {
      const ssoAdminPage = ssoAdminContext!.pages()[0]!;
      const result = await apiFetchInPage(ssoAdminPage, "PATCH", "/api/v0/auth/admin/sign-in-policy", {
        require_sso: true,
      });
      if (result.status !== 200) {
        throw new Error(
          `expected 200 from PATCH require_sso=true once both guardrail preconditions are proven, got ${result.status}: ${result.text}`,
        );
      }
      return `PATCH require_sso=true accepted (200): ${result.text.slice(0, 160)}`;
    });

    await step("item5_login_hides_local_form", async () => {
      const freshContext = await browser!.newContext({ baseURL: devServer!.baseUrl });
      try {
        const freshPage = await freshContext.newPage();
        await freshPage.goto("/login", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
        await freshPage.waitForSelector(".btn-sso", { timeout: navTimeoutMs });
        const loginFieldCount = await freshPage.locator("#login-id").count();
        if (loginFieldCount !== 0) {
          throw new Error("#login-id is present on /login even though require_sso=true");
        }
        await freshPage.screenshot({ path: resolve(screenshotsDir, "5-require-sso-hides-local.png"), fullPage: true });
        return "local password form absent on /login with require_sso=true";
      } finally {
        await freshContext.close();
      }
    });

    await step("item5_local_breakglass_still_works", async () => {
      const breakglassContext = await browser!.newContext({ baseURL: devServer!.baseUrl });
      try {
        const breakglassPage = await breakglassContext.newPage();
        await breakglassPage.goto("/login?local=1", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
        await breakglassPage.waitForSelector("#login-id", { timeout: navTimeoutMs });
        await driveLocalLogin(breakglassPage, credentialUsername, wizardNewPassword);
        await breakglassPage.screenshot({
          path: resolve(screenshotsDir, "5-breakglass-local-login.png"),
          fullPage: true,
        });
        return `?local=1 rendered #login-id and admin '${credentialUsername}' signed in locally`;
      } finally {
        await breakglassContext.close();
      }
    });

    if (memberContext) await memberContext.close();
    if (ssoAdminContext) await ssoAdminContext.close();

    const failed = results.filter((r) => r.status === "fail");
    const totalMs = Date.now() - runStart;
    await writeFile(
      reportPath,
      JSON.stringify({ apiBase, totalMs, results }, null, 2),
      "utf8",
    );
    process.stdout.write(
      `auth-e2e: ${results.length - failed.length}/${results.length} steps passed in ${totalMs}ms; report ${reportPath}\n`,
    );
    return failed.length > 0 ? 1 : 0;
  } finally {
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    if (devServer) {
      await stopAuthE2EDevServer(devServer);
    }
  }
}
