// authE2EOidcFlow.ts — OIDC-specific browser-driving helpers for the
// browser-auth E2E runner (issue #4971 phase 3, epic #4962 closer). Split out
// of runAuthE2E.ts to keep that file under the repository's 500-line limit;
// see its header comment for the overall acceptance-item map and the
// reachability decision this module's chromiumLaunchArgs implements.
//
// Two OIDC providers are in play by the time item 5 runs, reached two
// different ways:
//   - The member-mapped, DB-backed provider item 3 creates through the real
//     Add-provider UI (AdminProvidersPanel/ProviderConfigDrawer). Its login
//     button (the only one active at that point) is driven via driveOidcLogin.
//   - The admin-mapped, env/file-backed provider (pc_e2e_admin_static,
//     apps/console/e2e/fixtures/oidc-static-config.json). It deliberately has
//     NO backing DB row: go/internal/query/admin_provider_config_mutations.go's
//     rejectIfEnvManaged blocks Enable/Update/Disable for any provider_config_id
//     registered via ESHU_AUTH_OIDC_CONFIG_FILE ("managed by environment; edit
//     in your IaC, not here" — verified live, a 400 from a real run), and
//     go/internal/storage/postgres/identity_saml_sql.go's
//     selectActiveOIDCProviderConfigForTenantQuery requires `status='active'`
//     — so an env-shadowed row can never reach the state that would make it
//     appear on GET /api/v0/auth/providers (and thus render an on-page
//     button). But GET /api/v0/auth/oidc/login (go/internal/query/
//     oidc_login_handler.go's handleStart) never consults that listing at
//     all — it calls Service.provider(), which checks the env config's
//     s.config.Providers list FIRST and unconditionally (go/internal/
//     oidclogin/service.go), with no DB row involved. So this provider is
//     driven by navigating directly to that login-start URL
//     (driveDirectOidcLogin) rather than clicking a button that can never
//     exist — a real, production-shaped way to reach it (a bookmarked or
//     deep-linked SSO URL), not a test-only workaround.
import type { Page, Response } from "playwright";

const navTimeoutMs = 30000;

// mockOidcHostname/mockOidcAdminHostname are the in-network Compose service
// names (docker-compose.e2e.yaml) used as both mock IdPs' issuer — see that
// file's "Phase 3 reachability decision" comment.
const mockOidcHostname = "mock-oidc-idp";
const mockOidcAdminHostname = "mock-oidc-idp-admin";

// chromiumLaunchArgs maps the two in-network-only mock IdP hostnames to their
// host-published ports via Chromium's --host-resolver-rules flag, so the
// HOST Playwright browser resolves the exact same hostname:port the eshu API
// container already resolves natively over the Compose network — without any
// host OS /etc/hosts mutation. Scoped to this one Chromium process; see
// docker-compose.e2e.yaml's mock-oidc-idp comment for why this was chosen
// over host.docker.internal or a fully containerized browser.
export function chromiumLaunchArgs(mockOidcPort: string, mockOidcAdminPort: string): string[] {
  const rules = [
    `MAP ${mockOidcHostname} 127.0.0.1:${mockOidcPort}`,
    `MAP ${mockOidcAdminHostname} 127.0.0.1:${mockOidcAdminPort}`,
  ].join(", ");
  return [`--host-resolver-rules=${rules}`];
}

// apiFetchInPage performs an authenticated fetch against /eshu-api from
// WITHIN the page's own JS context, so it reuses the real browser session
// cookie and reads the real CSRF cookie the same way apps/console/src/api/
// client.ts's readCsrfCookie does (prefer __Host--prefixed, fall back to the
// insecure name CookieSecureAuto issues on a plain-HTTP loopback origin —
// docs #4964). Mirrors runAuthE2E.ts's readCsrfAndPatchSignInPolicy, made
// generic over method/path/body for reuse across every admin API call this
// module needs to make.
export async function apiFetchInPage(
  page: Page,
  method: string,
  path: string,
  body?: Record<string, unknown>,
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ([m, p, b]: readonly [string, string, string | undefined]) => {
      function readCookie(name: string): string {
        const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
        return match ? decodeURIComponent(match[1]) : "";
      }
      const csrf = readCookie("__Host-eshu_csrf") || readCookie("eshu_csrf");
      const init: RequestInit = {
        method: m,
        // credentials:"include" is belt-and-suspenders here: this fetch's
        // target ("/eshu-api/...") is always same-origin with the page that
        // issues it, so the "same-origin" default already sends the session
        // cookie — but being explicit removes any ambiguity from a caller
        // one day fetching a different origin.
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Eshu-CSRF": csrf },
      };
      if (b !== undefined) init.body = b;
      const res = await fetch(`/eshu-api${p}`, init);
      return { status: res.status, text: await res.text() };
    },
    [method, path, body === undefined ? undefined : JSON.stringify(body)] as const,
  );
}

// driveAddOidcProviderViaUI opens Admin -> Identity & Access -> Providers,
// fills the OIDC Add-provider form with the given fields, runs a test
// sign-in (asserting it passes), then saves (asserting the provider becomes
// active) — the real UI path acceptance item 3 requires. Field selectors
// mirror OidcProviderFields.tsx's aria-labels exactly.
export async function driveAddOidcProviderViaUI(
  page: Page,
  opts: {
    readonly issuer: string;
    readonly clientId: string;
    readonly clientSecret: string;
    readonly scopesText: string;
    readonly groupClaim: string;
  },
): Promise<void> {
  await page.goto("/admin", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".identity-access-panel", { timeout: navTimeoutMs });
  await page.click('button:has-text("Add provider")');
  await page.waitForSelector('[aria-label="Add provider"]', { timeout: navTimeoutMs });

  await page.fill('[aria-label="Issuer"]', opts.issuer);
  await page.fill('[aria-label="Client ID"]', opts.clientId);
  await page.fill('[aria-label="Client secret"]', opts.clientSecret);
  await page.fill('[aria-label="Scopes"]', opts.scopesText);
  await page.fill('[aria-label="Group claim"]', opts.groupClaim);

  // Live capture around the click: browser console/page errors and every
  // network response touching provider-configs. This is the ONLY way to see
  // what the drawer's own fetch actually did — a direct curl/API repro
  // proves the backend is fine but says nothing about whether the browser's
  // click even fired a request, and if so, what it got back.
  const captured: string[] = [];
  const onConsole = (msg: { type: () => string; text: () => string }): void => {
    captured.push(`console.${msg.type()}: ${msg.text()}`);
  };
  const onPageError = (err: Error): void => {
    captured.push(`pageerror: ${err.message}`);
  };
  const onResponse = (resp: { url: () => string; status: () => number }): void => {
    if (resp.url().includes("provider-configs") || resp.url().includes("test-connection")) {
      captured.push(`response: ${resp.status()} ${resp.url()}`);
    }
  };
  const onRequestFailed = (req: { url: () => string; failure: () => { errorText: string } | null }): void => {
    if (req.url().includes("provider-configs") || req.url().includes("test-connection")) {
      captured.push(`requestfailed: ${req.url()} — ${req.failure()?.errorText ?? "unknown"}`);
    }
  };
  page.on("console", onConsole);
  page.on("pageerror", onPageError);
  page.on("response", onResponse);
  page.on("requestfailed", onRequestFailed);

  // Confirm the button is actually enabled before clicking — oidcFormValid
  // (providerConfigForm.ts) disables it until issuer/clientId/clientSecret
  // are all non-empty; if page.fill() somehow didn't land in React state,
  // this catches it directly instead of inferring it from a later timeout.
  const testButton = page.locator('button:has-text("Run test sign-in")');
  const disabledBefore = await testButton.isDisabled();
  captured.push(`"Run test sign-in" button disabled before click: ${disabledBefore}`);

  // Deterministic gate #1: the test-connection network response itself, NOT
  // the transient ".provider-test-result" DOM node. ProviderConfigDrawer's
  // onRunTest calls saveDraft() (create/update) THEN
  // testProviderConfigConnection() and only afterward calls setTestResult()
  // followed synchronously by onSaved() (which bumps AdminProvidersPanel's
  // refreshKey and re-renders it) — a real run showed the backend round trip
  // completing (200s on both calls) while a hard wait on the transient result
  // paragraph still timed out, so this asserts against the actual HTTP
  // response instead of a DOM node that can race a parent re-render.
  let testConnectionResponse: Response;
  try {
    [testConnectionResponse] = await Promise.all([
      page.waitForResponse(
        (resp) => resp.url().includes("/test-connection") && resp.request().method() === "POST",
        { timeout: navTimeoutMs },
      ),
      page.click('button:has-text("Run test sign-in")'),
    ]);
  } catch (err) {
    page.off("console", onConsole);
    page.off("pageerror", onPageError);
    page.off("response", onResponse);
    page.off("requestfailed", onRequestFailed);
    const captureDump =
      captured.length > 0 ? captured.join(" | ") : "(no console/network/error events captured at all)";
    throw new Error(
      `test-connection network response never arrived after clicking "Run test sign-in": ${err instanceof Error ? err.message : String(err)}. captured: ${captureDump}`,
    );
  }

  page.off("console", onConsole);
  page.off("pageerror", onPageError);
  page.off("response", onResponse);
  page.off("requestfailed", onRequestFailed);
  const captureDump = captured.length > 0 ? captured.join(" | ") : "(no console/network/error events captured at all)";
  process.stdout.write(`  [item3 capture] ${captureDump}\n`);

  if (testConnectionResponse.status() !== 200) {
    throw new Error(
      `test-connection returned ${testConnectionResponse.status()} instead of 200. captured: ${captureDump}`,
    );
  }
  const testConnectionBody: { ok?: boolean; detail?: string } = await testConnectionResponse
    .json()
    .catch(() => ({}) as { ok?: boolean; detail?: string });
  if (testConnectionBody.ok !== true) {
    throw new Error(
      `test-connection responded 200 but body.ok was not true (${JSON.stringify(testConnectionBody)}). captured: ${captureDump}`,
    );
  }

  // Deterministic gate #2: the drawer's own React state must have committed
  // testResult before Save is clicked, because onSave's enable branch reads
  // testResult?.ok from local state, not from the network response above.
  // onRunTest's continuation calls setTestResult(...) then setTesting(false)
  // as sibling statements (React 18 batches them into one commit), so the
  // Save button re-enabling (busy flips back to false) is a reliable proxy
  // for testResult already being committed — no field is touched in between,
  // so the OidcProviderFields onChange handler never resets it back to null.
  await page.waitForFunction(
    () => {
      const buttons = Array.from(document.querySelectorAll(".drawer button"));
      const saveButton = buttons.find((b) => b.textContent?.trim() === "Save");
      return Boolean(saveButton) && !(saveButton as HTMLButtonElement).disabled;
    },
    undefined,
    { timeout: navTimeoutMs },
  );

  // ProviderConfigDrawer.onSave never auto-closes the drawer on success — it
  // saves again (a new revision superseding the just-tested draft, since
  // updateProviderConfig always creates one; fields are unchanged so it tests
  // identically), enables (the server re-tests that new active revision
  // synchronously — admin_provider_config_mutations.go's handleStatusChange —
  // and only activates it if that passes), and shows a "Saved and enabled"
  // notice in place, leaving the drawer open. Wait for that notice, then
  // close the drawer explicitly (the real close button, not Escape, to match
  // a real operator's next click).
  await page.click('.drawer button:has-text("Save")');
  await page.waitForSelector('.drawer p[role="status"]:has-text("Saved and enabled")', {
    timeout: navTimeoutMs,
  });
  await page.click('.drawer button[aria-label="Close"]');
  await page.waitForSelector(".drawer", { state: "detached", timeout: navTimeoutMs });
}

// assertProviderRowActive re-opens /admin's Providers tab and asserts the
// given provider now shows an "active" status badge and its row is present —
// the acceptance item 3 assertion that the provider "shows active."
export async function assertProviderRowActive(page: Page, issuerOrLabelText: string): Promise<void> {
  await page.goto("/admin", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector('table[aria-label="Providers"]', { timeout: navTimeoutMs });
  const row = page.locator('table[aria-label="Providers"] tr', { hasText: issuerOrLabelText });
  await row.waitFor({ state: "visible", timeout: navTimeoutMs });
  const activeBadgeCount = await row.getByText("active", { exact: true }).count();
  if (activeBadgeCount === 0) {
    throw new Error(`provider row for ${issuerOrLabelText} is not showing an "active" status badge`);
  }
}

// findProviderConfigIdByIssuer looks up the provider_config_id of the
// DB-backed provider whose stored issuer matches exactly, and asserts it is
// "active" via the admin API — the deterministic, server-side confirmation
// that driveAddOidcProviderViaUI's Save actually enabled the provider (not
// merely that its own UI-level waits resolved). The group-mapping step below
// requires an active provider (CompleteOIDCLogin only grants a login for an
// active revision), so this is asserted here, before that step runs, rather
// than discovered later as an opaque mapping failure. Needed because
// ProviderConfigDrawer generates the id client-side
// (newClientProviderConfigId(), apps/console/src/api/adminProviderConfig.ts)
// when created through the Add-provider UI — driveAddOidcProviderViaUI never
// surfaces it, so the group-mapping step below looks it up the same way an
// operator would (reading the admin provider list) rather than threading a
// pre-chosen id through the UI flow.
export async function findProviderConfigIdByIssuer(page: Page, issuer: string): Promise<string> {
  const result = await apiFetchInPage(page, "GET", "/api/v0/auth/admin/provider-configs");
  if (result.status >= 300) {
    throw new Error(`list provider configs failed (${result.status}): ${result.text}`);
  }
  const parsed: {
    provider_configs?: readonly { provider_config_id: string; status?: string; configuration?: { issuer?: string } }[];
  } = JSON.parse(result.text || "{}");
  const match = (parsed.provider_configs ?? []).find((item) => item.configuration?.issuer === issuer);
  if (!match) {
    throw new Error(`no provider config found with issuer ${issuer}`);
  }
  if (match.status !== "active") {
    throw new Error(
      `provider config ${match.provider_config_id} for issuer ${issuer} is not active (status=${match.status}) — group mapping requires an active provider`,
    );
  }
  return match.provider_config_id;
}

// createMemberGroupRoleMapping maps the member-mapped provider's external
// group to the ONLY role_id that exists in a fresh tenant: "owner"
// (pgstatus.localIdentityOwnerRoleID, seeded once at setup-claim time — see
// go/internal/storage/postgres/identity_local.go). There is currently no
// admin API to create a NEW role (AdminRolesPanel.tsx is read-only; grep
// confirms no INSERT into identity_role_grants exists anywhere in this
// codebase), so "owner" is the only value activeRoleExists
// (go/internal/storage/postgres/identity_admin_mutations.go) will accept.
// This does NOT grant the member session admin power: unlike local/SAML
// login, the DB-backed OIDC grant path (postgresOIDCStoreAdapter.
// ResolveGroupGrants, go/cmd/api/oidc_login.go) never runs the special
// role_id IN ('owner','tenant_admin') AllScopes check — that check is
// SQL-literal to the local/SAML queries only (identity_local_sql.go/
// identity_saml_sql.go) — and identity_role_grants has zero rows for ANY
// role_id in this codebase's current state, so resolvePermissionGrantsForRoles
// returns empty features regardless of which role_id resolves. The member
// session this produces is genuinely unprivileged: AllScopes=false,
// PermissionCatalogEnforced=true, AllowedPermissionFeatures=[] — exactly the
// non-admin shape item 4 asserts, achieved with the only role_id available
// rather than an invented one.
//
// Without ANY group mapping the member's login would be REJECTED outright
// (go/internal/oidclogin/service.go's CompleteOIDCLogin: `if !ok ||
// len(grants.RoleIDs) == 0 { return ..., ErrOIDCLoginDenied }`) — a mapping
// is required for the login to succeed at all, not merely for permissions.
export async function createMemberGroupRoleMapping(
  page: Page,
  providerConfigId: string,
  externalGroup: string,
): Promise<void> {
  const result = await apiFetchInPage(page, "POST", "/api/v0/auth/admin/idp-group-mappings", {
    provider_config_id: providerConfigId,
    external_group: externalGroup,
    role_id: "owner",
  });
  if (result.status >= 300) {
    throw new Error(`create idp group mapping (${externalGroup} -> owner) failed (${result.status}): ${result.text}`);
  }
}

// driveOidcLogin navigates to /login and clicks the (only) `.btn-sso`
// button, then waits for the post-login dashboard to render — proving the
// full redirect -> mock IdP -> callback -> session round trip completed.
// Only ever one provider is listed at the point this is used (the
// member-mapped one); the admin-mapped provider is driven directly via
// driveDirectOidcLogin instead (see this module's header comment for why it
// has no on-page button to click).
export async function driveOidcLogin(page: Page): Promise<void> {
  await page.goto("/login", { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.locator(".btn-sso").first().waitFor({ state: "visible", timeout: navTimeoutMs });
  await page.locator(".btn-sso").first().click();
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
}

// driveDirectOidcLogin navigates straight to GET /api/v0/auth/oidc/login for
// the given provider_config_id — the exact URL beginOidcLogin
// (apps/console/src/api/authSession.ts) would construct for an on-page
// button, skipped here because the env/file-backed admin provider has no
// button to click (see this module's header comment). The API's login-start
// handler (go/internal/query/oidc_login_handler.go) redirects straight to
// the mock IdP's /authorize; page.goto follows the whole
// authorize -> callback redirect chain as one top-level navigation.
export async function driveDirectOidcLogin(page: Page, providerConfigId: string): Promise<void> {
  const url = `/eshu-api/api/v0/auth/oidc/login?provider_config_id=${encodeURIComponent(providerConfigId)}&return_to=%2F`;
  await page.goto(url, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
}

// driveLocalLogin fills and submits the local password form (#login-id /
// #login-password), for the break-glass ?local=1 assertion.
export async function driveLocalLogin(page: Page, login: string, password: string): Promise<void> {
  await page.fill("#login-id", login);
  await page.fill("#login-password", password);
  await page.click('button[type="submit"].btn-primary');
  await page.waitForSelector(".source-pill.src-connected", { timeout: navTimeoutMs });
}
