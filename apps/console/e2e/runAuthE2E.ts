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
// Acceptance coverage (issue #4971 — all six items, one fresh zero-identity stack):
//   1. Fresh stack shows the SetupPage, not a dead-end LoginPage.
//   2. Claim the generated one-time credential, complete the 3-step wizard, land
//      on the dashboard; the credential is then destroyed (second retrieval empty)
//      and the setup routes return 410.
//   3. Configure a member-mapped OIDC provider through the real Add-provider UI
//      (add -> test -> enable); it shows active and appears on /login.
//   4. Complete a real browser OIDC redirect -> mock IdP -> callback as that
//      member (non-admin): no Admin nav, /admin renders the 403 AccessDeniedPage,
//      admin APIs 403.
//   5. The require_sso guardrail rejects a premature enable (400) until a provider
//      passes its test AND an admin has completed an SSO sign-in; enabling it then
//      revokes item 2's still-open, pre-existing LOCAL admin session (issue #5002 —
//      a 401 on that session proves the flip revoked an already-issued session, not
//      merely blocked future ones), hides the local form on /login, while
//      break-glass /login?local=1 still works (a NEW, untouched session). The
//      admin-SSO precondition needs a second, env/file-backed provider — a
//      DB-backed group mapping can never mint an AllScopes session; see
//      authE2EOidcFlow.ts and authE2ERequireSSOFlow.ts.
//   6. Negative-leakage scan (authE2ELeakage.ts): the flow's secrets (bootstrap
//      password/recovery code, wizard password, enrolled MFA code, both client
//      secrets) appear in NO audit trail, provider-config read, status/health,
//      DOM, or API container log — bar epic #4962's one-time banner.
// A CI job (frontend.yml's auth-sso-e2e) runs this whole gate on a fresh stack.
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser } from "playwright";

import {
  buildPostgresDSN,
  e2eDefaultAuthSecretEncKey,
  retrieveInitialCredential,
} from "./authE2ECredential.ts";
import {
  startAuthE2EDevServer,
  stopAuthE2EDevServer,
  type AuthE2EDevServer,
} from "./authE2EDevServer.ts";
import { runLeakageScan, type SecretProbe } from "./authE2ELeakage.ts";
import {
  assertFreshStackShowsSetupWizard,
  assertSetupRouteGone,
  driveSetupWizard,
} from "./authE2ESetupWizard.ts";
import {
  apiFetchInPage,
  assertProviderRowActive,
  chromiumLaunchArgs,
  createMemberGroupRoleMapping,
  driveAddOidcProviderViaUI,
  driveOidcLogin,
  findProviderConfigIdByIssuer,
} from "./authE2EOidcFlow.ts";
import {
  assertBreakglassLocalLoginStillWorks,
  assertGuardrailRejectsPrematureEnable,
  assertLoginHidesLocalForm,
  assertPreexistingLocalSessionRevoked,
  completeAdminSSOLoginPrecondition,
  enableRequireSSO,
} from "./authE2ERequireSSOFlow.ts";

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
const authSecretEncKey =
  (process.env.ESHU_AUTH_SECRET_ENC_KEY ?? "").trim() || e2eDefaultAuthSecretEncKey;
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
      await assertFreshStackShowsSetupWizard(page, devServer!.baseUrl, navTimeoutMs);
      await page.screenshot({
        path: resolve(screenshotsDir, "1-setup-wizard.png"),
        fullPage: true,
      });
      return "SetupPage rendered on first navigation; no #login-id field present";
    });

    let credentialUsername = "";
    // The setup wizard forces a password change (driveSetupWizard fills
    // #setup-new-password/#setup-confirm-password), so the credential this
    // captures is the ONE-TIME bootstrap password — item 5's break-glass
    // local-login assertion below uses the wizard's chosen replacement
    // instead (module-level wizardNewPassword), plus the wizard's real,
    // single-use MFA recovery code (see driveSetupWizard's doc comment).
    let breakglassRecoveryCode = "";
    // Captured for item 6's negative-leakage scan: the one-time bootstrap
    // password and its sealed recovery code must never surface in logs, audit
    // payloads, status endpoints, API responses, or the DOM.
    let bootstrapPassword = "";
    let bootstrapRecoveryCode = "";
    await step("item2_retrieve_initial_credential", async () => {
      const { credential, rawStderr } = await retrieveInitialCredential(
        repoGoDir,
        postgresDSN,
        authSecretEncKey,
      );
      if (!credential) {
        throw new Error(
          `eshu admin initial-credential returned nothing on a fresh stack: ${rawStderr}`,
        );
      }
      credentialUsername = credential.username;
      bootstrapPassword = credential.password;
      bootstrapRecoveryCode = credential.recoveryCode;
      breakglassRecoveryCode = await driveSetupWizard(
        page,
        credential.username,
        credential.password,
        wizardNewPassword,
        navTimeoutMs,
      );
      await page.screenshot({ path: resolve(screenshotsDir, "2-dashboard.png"), fullPage: true });
      return `wizard completed as ${credential.username}; dashboard reached (.source-pill.src-connected)`;
    });

    await step("item2_credential_consumed", async () => {
      const { credential, rawStderr } = await retrieveInitialCredential(
        repoGoDir,
        postgresDSN,
        authSecretEncKey,
      );
      if (credential) {
        throw new Error(
          "eshu admin initial-credential still returns a value after setup completed — not consumed",
        );
      }
      return `second retrieval correctly empty: ${rawStderr.trim().slice(0, 160)}`;
    });

    await step("item2_setup_routes_gone", async () => {
      await assertSetupRouteGone(apiBase);
      return "POST /api/v0/auth/setup/claim now returns 410 Gone";
    });

    await step("item5_guardrail_rejects_premature_enable", () =>
      assertGuardrailRejectsPrematureEnable(page),
    );

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
      await page.screenshot({
        path: resolve(screenshotsDir, "3-provider-active.png"),
        fullPage: true,
      });
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
        await loginPage.goto("/login?tenant_id=default", {
          waitUntil: "domcontentloaded",
          timeout: navTimeoutMs,
        });
        await loginPage.waitForSelector(".btn-sso", { timeout: navTimeoutMs });
        const count = await loginPage.locator(".btn-sso").count();
        if (count < 1) {
          throw new Error(
            "no SSO button rendered on /login after enabling the member OIDC provider",
          );
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
      await memberPage.screenshot({
        path: resolve(screenshotsDir, "4-member-dashboard.png"),
        fullPage: true,
      });
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
        throw new Error(
          "member session's /admin navigation rendered the admin shell, not AccessDeniedPage",
        );
      }
      return "GET /admin rendered AccessDeniedPage (403 screen), not the admin shell";
    });

    await step("item4_member_admin_api_call_403s", async () => {
      const memberPage = memberContext!.pages()[0]!;
      const result = await apiFetchInPage(memberPage, "GET", "/api/v0/auth/admin/provider-configs");
      if (result.status !== 403) {
        throw new Error(
          `expected 403 from an admin API call under the member session, got ${result.status}`,
        );
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
      const { context, detail } = await completeAdminSSOLoginPrecondition(
        browser!,
        devServer!.baseUrl,
        adminStaticProviderId,
      );
      ssoAdminContext = context;
      return detail;
    });

    await step("item5_enable_require_sso", () => enableRequireSSO(ssoAdminContext!.pages()[0]!));

    // Proves issue #5002: the flip above must revoke item 2's still-open,
    // pre-existing local admin session (subject_class='local_user'), not
    // just block future local logins — see
    // authE2ERequireSSOFlow.ts's doc comment.
    await step("item5_require_sso_flip_revokes_preexisting_local_session", () =>
      assertPreexistingLocalSessionRevoked(page),
    );

    await step("item5_login_hides_local_form", () =>
      assertLoginHidesLocalForm(browser!, devServer!.baseUrl, navTimeoutMs, screenshotsDir),
    );

    await step("item5_local_breakglass_still_works", () =>
      assertBreakglassLocalLoginStillWorks(
        browser!,
        devServer!.baseUrl,
        navTimeoutMs,
        screenshotsDir,
        credentialUsername,
        wizardNewPassword,
        breakglassRecoveryCode,
      ),
    );

    // item 6: negative-leakage scan. Runs while the member and admin sessions
    // are still open so both dashboards' DOM can be read. Proves none of the
    // secrets the flow generated or handled — the one-time bootstrap password
    // and its recovery code, the wizard's replacement password, the enrolled
    // MFA recovery code, and both providers' client secrets — appear in the
    // audit trail, provider-config reads, status/health endpoints, either
    // dashboard's DOM, or the API container log.
    await step("item6_no_secret_leakage", async () => {
      if (!memberContext) {
        throw new Error("item 6 requires the member session from item 4, but it was never opened");
      }
      const probes: SecretProbe[] = [
        { label: "bootstrap password", value: bootstrapPassword },
        { label: "bootstrap recovery code", value: bootstrapRecoveryCode },
        { label: "wizard replacement password", value: wizardNewPassword },
        { label: "enrolled MFA recovery code", value: breakglassRecoveryCode },
        { label: "member provider client secret", value: "unused-member-provider-secret-e2e" },
        {
          label: "admin static provider client secret",
          value: "unused-static-provider-secret-e2e",
        },
      ];
      return runLeakageScan({
        adminPage: page,
        memberPage: memberContext.pages()[0]!,
        apiFetchInPage,
        apiBase,
        repoRoot,
        project: (process.env.ESHU_E2E_PROJECT_NAME ?? "eshu-e2e-auth").trim(),
        probes,
      });
    });

    if (memberContext) await memberContext.close();
    if (ssoAdminContext) await ssoAdminContext.close();

    const failed = results.filter((r) => r.status === "fail");
    const totalMs = Date.now() - runStart;
    await writeFile(reportPath, JSON.stringify({ apiBase, totalMs, results }, null, 2), "utf8");
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
