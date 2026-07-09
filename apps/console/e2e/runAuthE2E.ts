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
// Items 3, 4, 6, and the CI job are out of scope for this runner; see the
// TODO markers below.
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright";

import { buildPostgresDSN, e2eDefaultAuthSecretEncKey, retrieveInitialCredential } from "./authE2ECredential.ts";
import { startAuthE2EDevServer, stopAuthE2EDevServer, type AuthE2EDevServer } from "./authE2EDevServer.ts";

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
const devServerPort = 5185;
const navTimeoutMs = 30000;

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

function blocked(id: string, detail: string): void {
  results.push({ id, status: "blocked", detail, ms: 0 });
  process.stdout.write(`  BLOCKED ${id}: ${detail}\n`);
}

// readCsrfCookieScript mirrors apps/console/src/api/client.ts's
// readCsrfCookie(): prefer the __Host--prefixed cookie name, fall back to
// the insecure name CookieSecureAuto issues on a plain-HTTP loopback origin
// (docs #4964). Evaluated inside the page so it reads the real browser
// cookie jar the setup wizard's session left behind.
const readCsrfAndPatchSignInPolicy = async (
  page: Page,
  body: Record<string, unknown>,
): Promise<{ status: number; text: string }> =>
  page.evaluate(async (requestBody) => {
    function readCookie(name: string): string {
      const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
      return match ? decodeURIComponent(match[1]) : "";
    }
    const csrf = readCookie("__Host-eshu_csrf") || readCookie("eshu_csrf");
    const res = await fetch("/eshu-api/api/v0/auth/admin/sign-in-policy", {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Eshu-CSRF": csrf },
      body: JSON.stringify(requestBody),
    });
    return { status: res.status, text: await res.text() };
  }, body);

async function assertFreshStackShowsSetupWizard(page: Page, baseUrl: string): Promise<void> {
  await page.addInitScript(
    ([base]: readonly string[]) => {
      window.localStorage.setItem(
        "eshu.console.environment",
        JSON.stringify({ mode: "private", apiBaseUrl: base, recentApiBaseUrls: [base] }),
      );
    },
    ["/eshu-api/"],
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
  const newPassword = "E2E-auth-runner-P@ssw0rd-1";
  await page.fill("#setup-new-password", newPassword);
  await page.fill("#setup-confirm-password", newPassword);
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
    browser = await chromium.launch();
    const context = await browser.newContext();
    const page = await context.newPage();

    await step("item1_setup_wizard_renders", async () => {
      await assertFreshStackShowsSetupWizard(page, devServer!.baseUrl);
      await page.screenshot({ path: resolve(screenshotsDir, "1-setup-wizard.png"), fullPage: true });
      return "SetupPage rendered on first navigation; no #login-id field present";
    });

    let credentialUsername = "";
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
      const result = await readCsrfAndPatchSignInPolicy(page, { require_sso: true });
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

    blocked(
      "item5_login_hides_local_form_and_breakglass",
      "Needs require_sso=true to actually persist, which needs the guardrail's " +
        "two preconditions (a provider config with a passing connection test, and " +
        "an admin who has completed one SSO sign-in) — both require OIDC wired to " +
        "a browser-reachable mock IdP. That is issue #4971 phase 3 (items 3/4). " +
        "TODO(phase 3): after configuring + testing an OIDC provider and " +
        "completing one SSO login as the admin created above, PATCH " +
        "require_sso=true here, reload /login in a fresh browser context, assert " +
        "#login-id is absent, then assert /login?local=1 still renders #login-id " +
        `and admin '${credentialUsername}' can still sign in locally.`,
    );

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
