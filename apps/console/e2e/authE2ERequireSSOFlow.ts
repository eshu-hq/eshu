// authE2ERequireSSOFlow.ts — item 5 (require_sso guardrail, the flip itself,
// and its downstream login/session assertions) step bodies for the
// browser-auth E2E runner (issue #4971 phase 5, completed by issue #5002).
// Extracted from runAuthE2E.ts to keep that runner under the repository's
// 500-line cap; see its header comment for the overall acceptance-item map.
import { resolve } from "node:path";
import type { Browser, Page } from "playwright";

import { apiFetchInPage, driveDirectOidcLogin, driveLocalLogin } from "./authE2EOidcFlow.ts";

// assertGuardrailRejectsPrematureEnable drives PATCH .../sign-in-policy on
// the wizard admin's own (still-local) session BEFORE either require_sso
// guardrail precondition (a provider with a passing connection test, an
// admin SSO sign-in) is proven, and asserts the guardrail's 400 rejection.
export async function assertGuardrailRejectsPrematureEnable(page: Page): Promise<string> {
  const result = await apiFetchInPage(page, "PATCH", "/api/v0/auth/admin/sign-in-policy", {
    require_sso: true,
  });
  if (result.status !== 400) {
    throw new Error(
      `expected 400 from the require_sso guardrail with no proven provider/SSO admin, got ${result.status}: ${result.text}`,
    );
  }
  if (!result.text.includes("require_sso cannot be enabled")) {
    throw new Error(`400 body did not carry the expected guardrail message: ${result.text}`);
  }
  return `PATCH require_sso=true correctly rejected (400): ${result.text}`;
}

// completeAdminSSOLoginPrecondition completes an SSO login through the
// env/file-backed admin provider in a FRESH browser context — no DB row is
// created for it (see authE2EOidcFlow.ts's header comment: the admin
// mutation API refuses to manage an env-shadowed provider_config_id at all).
// This identity resolves to an AllScopes=true session purely through
// ESHU_AUTH_OIDC_CONFIG_FILE's role_grants (docker-compose.e2e.yaml),
// recording the guardrail's SSO-admin-proof precondition
// (sso_admin_verified_at). Returns the open context so the caller can reuse
// its page for the flip itself and close it once item 5 finishes.
export async function completeAdminSSOLoginPrecondition(
  browser: Browser,
  baseUrl: string,
  adminStaticProviderId: string,
): Promise<{ context: Awaited<ReturnType<Browser["newContext"]>>; detail: string }> {
  const context = await browser.newContext({ baseURL: baseUrl });
  const page = await context.newPage();
  await driveDirectOidcLogin(page, adminStaticProviderId);
  return {
    context,
    detail:
      "admin-mapped identity completed SSO login, recording sso_admin_verified_at for the tenant",
  };
}

// enableRequireSSO drives the actual flip: PATCH require_sso=true on the
// SSO-admin session, once both guardrail preconditions are proven.
export async function enableRequireSSO(ssoAdminPage: Page): Promise<string> {
  const result = await apiFetchInPage(ssoAdminPage, "PATCH", "/api/v0/auth/admin/sign-in-policy", {
    require_sso: true,
  });
  if (result.status !== 200) {
    throw new Error(
      `expected 200 from PATCH require_sso=true once both guardrail preconditions are proven, got ${result.status}: ${result.text}`,
    );
  }
  return `PATCH require_sso=true accepted (200): ${result.text.slice(0, 160)}`;
}

// assertPreexistingLocalSessionRevoked proves issue #5002: the require_sso
// false->true flip must bulk-revoke every ALREADY-ISSUED
// subject_class='local_user' browser session for the tenant, not merely
// block new local logins going forward. `page` here is item 2's ORIGINAL
// setup-wizard admin session — still open, never closed, authenticated
// purely via local username/password (identity_local.go's
// AuthenticateLocalIdentity sets subject_class='local_user' for every local
// sign-in, admin or not; only the break-glass ?local=1 path later sets
// subject_class='break_glass'). A GET against an authenticated admin route
// through THAT session must now 401 (session revoked), not 200 — proving
// the pre-existing session's revoked_at was actually set by
// UpsertSignInPolicy's bulk revoke (go/internal/storage/postgres/
// identity_sign_in_policy.go), not just that new local logins are rejected.
export async function assertPreexistingLocalSessionRevoked(page: Page): Promise<string> {
  const result = await apiFetchInPage(page, "GET", "/api/v0/auth/admin/sign-in-policy");
  if (result.status !== 401) {
    throw new Error(
      `expected 401 from item 2's pre-existing local admin session after the require_sso flip revoked it, got ${result.status}: ${result.text}`,
    );
  }
  return `pre-existing local admin session correctly revoked (401) after the require_sso flip: ${result.text.slice(0, 160)}`;
}

// assertLoginHidesLocalForm proves /login renders only the SSO button once
// require_sso=true, in a fresh (never-authenticated) context.
export async function assertLoginHidesLocalForm(
  browser: Browser,
  baseUrl: string,
  navTimeoutMs: number,
  screenshotsDir: string,
): Promise<string> {
  const freshContext = await browser.newContext({ baseURL: baseUrl });
  try {
    const freshPage = await freshContext.newPage();
    await freshPage.goto("/login?tenant_id=default", {
      waitUntil: "domcontentloaded",
      timeout: navTimeoutMs,
    });
    await freshPage.waitForSelector(".btn-sso", { timeout: navTimeoutMs });
    const loginFieldCount = await freshPage.locator("#login-id").count();
    if (loginFieldCount !== 0) {
      throw new Error("#login-id is present on /login even though require_sso=true");
    }
    await freshPage.screenshot({
      path: resolve(screenshotsDir, "5-require-sso-hides-local.png"),
      fullPage: true,
    });
    return "local password form absent on /login with require_sso=true";
  } finally {
    await freshContext.close();
  }
}

// assertBreakglassLocalLoginStillWorks proves ?local=1 still renders the
// local form and a real local login succeeds once require_sso=true.
// Break-glass mints a NEW session with subject_class='break_glass'
// (identity_local.go), which the require_sso flip's local_user-scoped
// revoke never touches — this is the counterpart proof to
// assertPreexistingLocalSessionRevoked: the flip revokes password sessions,
// not the break-glass escape hatch itself.
export async function assertBreakglassLocalLoginStillWorks(
  browser: Browser,
  baseUrl: string,
  navTimeoutMs: number,
  screenshotsDir: string,
  credentialUsername: string,
  wizardNewPassword: string,
  breakglassRecoveryCode: string,
): Promise<string> {
  const breakglassContext = await browser.newContext({ baseURL: baseUrl });
  try {
    const breakglassPage = await breakglassContext.newPage();
    await breakglassPage.goto("/login?tenant_id=default&local=1", {
      waitUntil: "domcontentloaded",
      timeout: navTimeoutMs,
    });
    await breakglassPage.waitForSelector("#login-id", { timeout: navTimeoutMs });
    await driveLocalLogin(
      breakglassPage,
      credentialUsername,
      wizardNewPassword,
      breakglassRecoveryCode,
    );
    await breakglassPage.screenshot({
      path: resolve(screenshotsDir, "5-breakglass-local-login.png"),
      fullPage: true,
    });
    return `?local=1 rendered #login-id and admin '${credentialUsername}' signed in locally`;
  } finally {
    await breakglassContext.close();
  }
}
