// First-run setup-phase helpers for the auth E2E (issue #4971). Extracted from
// runAuthE2E.ts to keep that runner under the 500-line cap; they drive and
// assert exactly the first-boot setup surface a real operator walks through.
import type { Page } from "playwright";

// assertFreshStackShowsSetupWizard proves a zero-identity stack renders the
// SetupPage (not a dead-end LoginPage) on first navigation, and seeds the
// console environment with the dev server's OWN absolute origin + "/eshu-api".
// That absolute apiBaseUrl matters because oidcRedirectUri()/samlAcsUrl()
// (apps/console/src/api/adminProviderConfig.ts) derive the OIDC/SAML redirect
// URIs registered with a provider from it, and those become literal `Location:`
// redirect targets a real browser navigates to. A relative "/eshu-api/..."
// redirect_uri would resolve against the IdP's OWN origin once the mock IdP 302s
// the browser there (go/cmd/mock-oidc-idp/server.go's url.Parse+http.Redirect
// accepts it verbatim), landing on the wrong host. Absolute is what item 3/4/5's
// real browser OIDC round trip needs; item 1/2 (local-only) are unaffected.
export async function assertFreshStackShowsSetupWizard(
  page: Page,
  baseUrl: string,
  navTimeoutMs: number,
): Promise<void> {
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
    throw new Error(
      "LoginPage's #login-id field is present alongside SetupPage — dead-end login form",
    );
  }
}

// assertSetupRouteGone proves the setup claim route returns 410 Gone once setup
// has completed, so it can never be replayed to re-seed an admin.
export async function assertSetupRouteGone(apiBase: string): Promise<void> {
  const res = await fetch(`${apiBase}/api/v0/auth/setup/claim`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: "irrelevant", password: "irrelevant" }),
  });
  if (res.status !== 410) {
    throw new Error(
      `expected 410 Gone from POST /api/v0/auth/setup/claim after setup, got ${res.status}`,
    );
  }
}

// driveSetupWizard completes the 3-step wizard and returns ONE of the real,
// plaintext MFA recovery codes the wizard's step 3 generates and displays
// (SetupPage.tsx's completeSetupMFA / SetupMFAStep.tsx's `ul.codes li`) —
// scraped from the DOM before "Finish setup" is clicked, the same value a
// real operator would copy/download to save. This is required because
// enrolling recovery codes here makes MFA mandatory on every subsequent
// local login for this account (verified live: a later plain login/password
// POST /api/v0/auth/local/login for this account returns 202
// {status:"mfa_required"}, not a session) — item5's break-glass local-login
// assertion (driveLocalLogin) needs a genuine, working code to get past that
// MFA step. Each code works once (SetupMFAStep.tsx), so callers must not
// reuse the returned code for more than one login. newPassword is the
// replacement the wizard forces (reused by item 5's break-glass login) and
// navTimeoutMs bounds every wait.
export async function driveSetupWizard(
  page: Page,
  username: string,
  password: string,
  newPassword: string,
  navTimeoutMs: number,
): Promise<string> {
  await page.fill("#setup-username", username);
  await page.fill("#setup-password", password);
  await page.click('.card-foot button[type="submit"]');

  await page.waitForSelector("#setup-new-password", { timeout: navTimeoutMs });
  await page.fill("#setup-new-password", newPassword);
  await page.fill("#setup-confirm-password", newPassword);
  await page.click('.card-foot button[type="submit"]');

  await page.waitForSelector("ul.codes li", { timeout: navTimeoutMs });
  const recoveryCode = (await page.locator("ul.codes li").first().textContent())?.trim() ?? "";
  if (recoveryCode === "") {
    throw new Error("wizard step 3 rendered ul.codes but the first recovery code was empty");
  }

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
  return recoveryCode;
}
