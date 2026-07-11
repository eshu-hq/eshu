// authE2ELocalMemberFlow.ts — LOCAL (password) non-admin member step bodies
// for the browser-auth E2E runner (issue #5073, extending issue #4971's
// browser-auth gate). Extracted from runAuthE2E.ts to keep that runner under
// the repository's 500-line cap; see its header comment for the overall
// acceptance-item map. Three real, browser-observable LOCAL member flows:
// each invitation is seeded directly on Postgres (HTTP-unreachable here —
// see createLocalInvitationDirect) and run through the real accept endpoint:
//
//   Flow A (#5001): a member accepted with NO MFA factor, once the tenant
//   turns on require_mfa_for_all_users, is blocked at LOGIN TIME (not merely
//   at invitation-accept time) — the login-time gate #5001 added on top of
//   the older accept-time-only check.
//   Flow B (#4986): a member enrolls TOTP (begin -> compute a live RFC 6238
//   code from the returned secret -> confirm); a fresh login with a valid
//   code authenticates and an invalid one is rejected.
//   Flow C (#4976): a member's active credential is forced into
//   must_change_password=true and must rotate before it gets a session; a
//   subsequent login with the new password (via a still-unused recovery
//   code) authenticates, so rotation neither breaks recovery-code login nor
//   strands the account.
//
// Ordering: every flow here needs require_sso=false — handleLogin's
// requireSSODecision (#4968) denies ALL non-admin local login once
// require_sso=true, collapsing every assertion below into an
// indistinguishable 403. Item 5 flips require_sso=true for the rest of the
// run and never reverts it, so runAuthE2E.ts calls this module strictly
// AFTER item 4 (member OIDC login) and BEFORE item 5's admin-SSO
// precondition/enable. Within that window every member is created (invited
// + accepted) and, for Flow B, logs in and enrolls TOTP, BEFORE
// require_mfa_for_all_users flips true: AcceptLocalIdentityInvitation
// rejects a no-recovery-codes acceptance once that policy is already on,
// and Flow B's enrollment login would itself demand an MFA proof the
// member lacks yet if the flip happened first. Flow C's member carries
// recovery codes at acceptance so it runs after the flip too.
import { createHash, randomUUID } from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { Browser, Page } from "playwright";

import { apiFetchInPage } from "./authE2EOidcFlow.ts";
import { generateTotpCode } from "./totpCode.ts";

const execFileAsync = promisify(execFile);

// LocalFlowContext bundles the 3 values every helper below needs to open a
// FRESH, never-authenticated browser context — passed once instead of
// threaded as 3 separate parameters through every call site.
export interface LocalFlowContext {
  readonly browser: Browser;
  readonly baseUrl: string;
  readonly navTimeoutMs: number;
}

export interface LocalMemberCredential {
  readonly loginId: string;
  readonly password: string;
}

export interface LocalMemberFlowBHandle extends LocalMemberCredential {
  readonly totpSecret: string;
  readonly totpDigits: number;
  readonly totpPeriodSeconds: number;
}
export interface LocalMemberFlowCHandle extends LocalMemberCredential {
  readonly recoveryCodes: readonly [string, string];
}

export interface ProvisionedLocalMembers {
  readonly flowA: LocalMemberCredential;
  readonly flowB: LocalMemberFlowBHandle;
  readonly flowC: LocalMemberFlowCHandle;
  readonly detail: string;
}

// openFreshMemberPage opens a brand-new browser context with zero cookies,
// navigated to the app origin first so apiFetchInPage's relative
// fetch("/eshu-api/...") resolves against the right origin before any
// session exists. Each assertion below opens its own fresh context per
// call, mirroring authE2ERequireSSOFlow.ts's "fresh context proves no
// session bleed-through" convention — reusing a page that already picked up
// a CSRF cookie would make the "no session issued" checks meaningless.
async function openFreshMemberPage(
  ctx: LocalFlowContext,
): Promise<{ context: Awaited<ReturnType<Browser["newContext"]>>; page: Page }> {
  const context = await ctx.browser.newContext({ baseURL: ctx.baseUrl });
  const page = await context.newPage();
  await page.goto("/login?tenant_id=default", {
    waitUntil: "domcontentloaded",
    timeout: ctx.navTimeoutMs,
  });
  return { context, page };
}

// hasSessionCsrfCookie is the observable proxy for "a session was issued":
// the session cookie is HttpOnly (invisible to document.cookie), but
// writeBrowserSessionCookies always sets the readable CSRF cookie
// (__Host-eshu_csrf or eshu_csrf) in the SAME response, never without it.
async function hasSessionCsrfCookie(page: Page): Promise<boolean> {
  const cookies = await page.evaluate(() => document.cookie);
  return cookies.includes("eshu_csrf=");
}

interface LocalLoginResponse {
  readonly status: string;
}

// assertNoSessionResult is the shared shape behind Flow A's and Flow C's
// "blocked, no session" checks: mfa_required and must_change_password both
// return the same LocalIdentitySessionResponse{Status} envelope with no
// session issued (writeLocalIdentityUnauthenticated) — only the expected
// HTTP status and status string differ.
async function assertNoSessionResult(
  page: Page,
  result: { readonly status: number; readonly text: string },
  wantStatus: number,
  wantBody: string,
): Promise<void> {
  if (result.status !== wantStatus) {
    throw new Error(`expected ${wantStatus} ${wantBody}, got ${result.status}: ${result.text}`);
  }
  const body: LocalLoginResponse = JSON.parse(result.text || "{}");
  if (body.status !== wantBody) {
    throw new Error(`expected status "${wantBody}", got ${JSON.stringify(body)}`);
  }
  if (await hasSessionCsrfCookie(page)) {
    throw new Error(`a session CSRF cookie was set despite ${wantBody} — no session issued`);
  }
}

// assertSessionIssuedResult is the shared shape behind every "authenticates"
// check below: HTTP 200, body status "authenticated", and the CSRF cookie
// writeBrowserSessionCookies always sets alongside a real session.
async function assertSessionIssuedResult(
  page: Page,
  result: { readonly status: number; readonly text: string },
  label: string,
): Promise<void> {
  if (result.status !== 200) {
    throw new Error(`expected 200 authenticated ${label}, got ${result.status}: ${result.text}`);
  }
  const body: LocalLoginResponse = JSON.parse(result.text || "{}");
  if (body.status !== "authenticated") {
    throw new Error(`expected status "authenticated" ${label}, got ${JSON.stringify(body)}`);
  }
  if (!(await hasSessionCsrfCookie(page))) {
    throw new Error(`no session CSRF cookie set after a 200 authenticated ${label}`);
  }
}

interface LocalMemberAcceptInput {
  readonly loginId: string;
  readonly password: string;
  readonly recoveryCodes: readonly string[];
}

// createLocalInvitationDirect seeds an ACTIVE identity_invitations row
// directly on Postgres in place of POST .../invitations, reachable only via
// AuthModeShared (unset in this zero-shared-key stack) per
// scopedAuthAdminMutationRoute; the console has no create-invitation UI
// either. Seeds it like seedMustChangePassword below (compose "postgres"'s
// psql). role_id fixed "developer" (non-admin); ACCEPT below stays real.
async function createLocalInvitationDirect(
  repoRoot: string,
  project: string,
  loginId: string,
): Promise<string> {
  const inviteCode = `e2e-invite-${randomUUID()}`;
  const inviteId = `e2e-inv-${randomUUID()}`;
  const inviteCodeHash = localIdentityHash(inviteCode);
  // policy_revision_hash is read inline (COALESCE '' if none); accept never validates it.
  const sql =
    "INSERT INTO identity_invitations (invite_id, tenant_id, workspace_id, invite_code_hash, " +
    "invitee_handle_hash, inviter_subject_id_hash, role_id, status, policy_revision_hash, " +
    "expires_at, accepted_by_user_id, accepted_at, revoked_at, tombstoned_at, created_at, updated_at) " +
    `VALUES ('${inviteId}', 'default', 'default', '${inviteCodeHash}', NULL, NULL, 'developer', ` +
    "'active', COALESCE((SELECT policy_revision_hash FROM tenants WHERE tenant_id = 'default'), ''), " +
    "now() + interval '1 hour', NULL, NULL, NULL, NULL, now(), now()) RETURNING invite_id;";
  const composeArgs = ["compose", "-p", project, "-f", "docker-compose.e2e.yaml"];
  const psqlArgs = ["exec", "-T", "postgres", "psql", "-U", "eshu", "-d", "eshu", "-tA", "-c", sql];
  const { stdout } = await execFileAsync("docker", [...composeArgs, ...psqlArgs], {
    cwd: repoRoot,
  });
  if (stdout.trim() === "") {
    throw new Error(`invitation seed insert for ${loginId} returned no invite_id`);
  }
  return inviteCode;
}

// createAndAcceptLocalMember seeds the invitation, then drives the real public accept route.
async function createAndAcceptLocalMember(
  repoRoot: string,
  project: string,
  ctx: LocalFlowContext,
  opts: LocalMemberAcceptInput,
): Promise<void> {
  const inviteCode = await createLocalInvitationDirect(repoRoot, project, opts.loginId);
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const accepted = await apiFetchInPage(page, "POST", "/api/v0/auth/local/invitations/accept", {
      invite_code: inviteCode,
      login_id: opts.loginId,
      password: opts.password,
      recovery_codes: opts.recoveryCodes,
    });
    if (accepted.status !== 201) {
      throw new Error(
        `invitation accept failed for ${opts.loginId} (${accepted.status}): ${accepted.text}`,
      );
    }
  } finally {
    await context.close();
  }
}

interface LocalTotpBeginResponse {
  readonly factor_id: string;
  readonly secret: string;
  readonly digits: number;
  readonly period_seconds: number;
}

// enrollTotpForFlowB logs the already-accepted, MFA-free flowB member in
// (require_mfa_for_all_users is still off — see header comment), then
// drives the real begin -> confirm TOTP enrollment (#4986) using a freshly
// computed RFC 6238 code, proving totpCode.ts matches the server's check.
async function enrollTotpForFlowB(
  ctx: LocalFlowContext,
  member: LocalMemberCredential,
): Promise<{ totpSecret: string; totpDigits: number; totpPeriodSeconds: number }> {
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const login = await apiFetchInPage(page, "POST", "/api/v0/auth/local/login", {
      login_id: member.loginId,
      password: member.password,
    });
    if (login.status !== 200) {
      throw new Error(`pre-enrollment login failed for ${member.loginId} (${login.status})`);
    }
    const begin = await apiFetchInPage(page, "POST", "/api/v0/auth/local/mfa/totp/begin", {
      account_label: member.loginId,
    });
    if (begin.status !== 201) {
      throw new Error(`totp begin failed for ${member.loginId} (${begin.status}): ${begin.text}`);
    }
    const beginBody: LocalTotpBeginResponse = JSON.parse(begin.text || "{}");
    const code = generateTotpCode(
      beginBody.secret,
      Math.floor(Date.now() / 1000),
      beginBody.digits,
      beginBody.period_seconds,
    );
    const confirm = await apiFetchInPage(page, "POST", "/api/v0/auth/local/mfa/totp/confirm", {
      factor_id: beginBody.factor_id,
      code,
    });
    if (confirm.status !== 204) {
      throw new Error(`totp confirm failed for ${member.loginId} (${confirm.status})`);
    }
    return {
      totpSecret: beginBody.secret,
      totpDigits: beginBody.digits,
      totpPeriodSeconds: beginBody.period_seconds,
    };
  } finally {
    await context.close();
  }
}

// provisionLocalMemberFlows creates the three non-admin LOCAL members Flows
// A/B/C need (see header comment for why all three precede the policy flip)
// and, for the Flow B member only, completes TOTP enrollment while a plain
// password login still works.
export async function provisionLocalMemberFlows(
  repoRoot: string,
  project: string,
  ctx: LocalFlowContext,
): Promise<ProvisionedLocalMembers> {
  const flowA: LocalMemberCredential = {
    loginId: "e2e-local-member-no-factor@example.test",
    password: "E2E-local-member-a-P@ssw0rd-1",
  };
  const flowBBase: LocalMemberCredential = {
    loginId: "e2e-local-member-totp@example.test",
    password: "E2E-local-member-b-P@ssw0rd-1",
  };
  const flowC: LocalMemberFlowCHandle = {
    loginId: "e2e-local-member-must-change@example.test",
    password: "E2E-local-member-c-P@ssw0rd-1",
    recoveryCodes: ["e2e-flowc-recovery-code-one", "e2e-flowc-recovery-code-two"],
  };

  await createAndAcceptLocalMember(repoRoot, project, ctx, { ...flowA, recoveryCodes: [] });
  await createAndAcceptLocalMember(repoRoot, project, ctx, { ...flowBBase, recoveryCodes: [] });
  await createAndAcceptLocalMember(repoRoot, project, ctx, {
    loginId: flowC.loginId,
    password: flowC.password,
    recoveryCodes: flowC.recoveryCodes,
  });
  const totp = await enrollTotpForFlowB(ctx, flowBBase);

  return {
    flowA,
    flowB: { ...flowBBase, ...totp },
    flowC,
    detail: `provisioned 3 local non-admin members via real invitation-accept (developer role): ${flowA.loginId} (no MFA factor), ${flowBBase.loginId} (TOTP enrolled), ${flowC.loginId} (2 recovery codes)`,
  };
}

// setRequireMfaForAllUsers flips the single tenant policy Flows A and B both
// depend on; require_sso=false is passed explicitly to document, not just
// assume, the precondition every flow in this module needs.
export async function setRequireMfaForAllUsers(adminPage: Page): Promise<string> {
  const result = await apiFetchInPage(adminPage, "PATCH", "/api/v0/auth/admin/sign-in-policy", {
    require_mfa_for_all_users: true,
    require_sso: false,
  });
  if (result.status !== 200) {
    throw new Error(`require_mfa_for_all_users PATCH failed (${result.status}): ${result.text}`);
  }
  return `PATCH require_mfa_for_all_users=true accepted (200): ${result.text.slice(0, 160)}`;
}

// assertNoFactorMemberMfaRequired is Flow A (#5001): the no-MFA-factor
// member, once require_mfa_for_all_users is on, must be blocked AT LOGIN —
// the !row.HasActiveMFA branch in AuthenticateLocalIdentity.
export async function assertNoFactorMemberMfaRequired(
  ctx: LocalFlowContext,
  member: LocalMemberCredential,
): Promise<string> {
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const result = await apiFetchInPage(page, "POST", "/api/v0/auth/local/login", {
      login_id: member.loginId,
      password: member.password,
    });
    await assertNoSessionResult(page, result, 202, "mfa_required");
    return `no-factor member login correctly returned 202 mfa_required, no session issued: ${result.text}`;
  } finally {
    await context.close();
  }
}

// assertTotpLoginSucceeds is Flow B's positive case: a freshly computed,
// currently-valid TOTP code authenticates and issues a session.
export async function assertTotpLoginSucceeds(
  ctx: LocalFlowContext,
  member: LocalMemberFlowBHandle,
): Promise<string> {
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const code = generateTotpCode(
      member.totpSecret,
      Math.floor(Date.now() / 1000),
      member.totpDigits,
      member.totpPeriodSeconds,
    );
    const result = await apiFetchInPage(page, "POST", "/api/v0/auth/local/login", {
      login_id: member.loginId,
      password: member.password,
      totp_code: code,
    });
    await assertSessionIssuedResult(page, result, "with a valid TOTP code");
    return `TOTP login with a freshly computed valid code authenticated (200), session issued`;
  } finally {
    await context.close();
  }
}

// assertTotpLoginRejectsInvalidCode is Flow B's negative case: a code
// computed ~333 steps outside the server's +/-1 step (30s) skew window
// (go/internal/totp.DefaultSkewSteps) can never coincide with a valid code
// by construction, and must be rejected without issuing a session.
export async function assertTotpLoginRejectsInvalidCode(
  ctx: LocalFlowContext,
  member: LocalMemberFlowBHandle,
): Promise<string> {
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const staleCode = generateTotpCode(
      member.totpSecret,
      Math.floor(Date.now() / 1000) - 10_000,
      member.totpDigits,
      member.totpPeriodSeconds,
    );
    const result = await apiFetchInPage(page, "POST", "/api/v0/auth/local/login", {
      login_id: member.loginId,
      password: member.password,
      totp_code: staleCode,
    });
    await assertNoSessionResult(page, result, 401, "invalid");
    return `TOTP login with a stale/out-of-window code correctly rejected (401), no session issued`;
  } finally {
    await context.close();
  }
}

// localIdentityHash mirrors local_identity_handler_helpers.go's
// localIdentityHash exactly (sha256 hex, "sha256:" prefixed, trimmed) — the
// store never persists a raw login_id, only this hash.
function localIdentityHash(value: string): string {
  const trimmed = value.trim();
  if (trimmed === "") return "";
  return `sha256:${createHash("sha256").update(trimmed).digest("hex")}`;
}

// seedMustChangePassword is Flow C's (#4976) deliberate substrate seed. No
// product API sets must_change_password=true for a MEMBER: admin password
// reset clears it, invitation-accept always inserts it false, and the only
// product path that ever produces it is the env-seeded bootstrap admin
// (identity_bootstrap_credential.go). So this seeds it directly on
// Postgres, over the same "postgres" compose service
// authE2ELeakage.ts's collectApiContainerLogs already reaches, shelling
// into the container's own bundled psql (postgres:18-alpine).
export async function seedMustChangePassword(
  repoRoot: string,
  project: string,
  loginId: string,
): Promise<string> {
  const subjectHash = localIdentityHash(loginId);
  const sql =
    "UPDATE identity_local_credentials AS c SET must_change_password = true " +
    "FROM identity_users AS u " +
    `WHERE c.user_id = u.user_id AND u.subject_id_hash = '${subjectHash}' ` +
    "AND c.status = 'active' AND c.revoked_at IS NULL RETURNING c.credential_id;";
  const composeArgs = ["compose", "-p", project, "-f", "docker-compose.e2e.yaml"];
  const psqlArgs = ["exec", "-T", "postgres", "psql", "-U", "eshu", "-d", "eshu", "-tA", "-c", sql];
  const { stdout } = await execFileAsync("docker", [...composeArgs, ...psqlArgs], {
    cwd: repoRoot,
  });
  const credentialID = stdout.trim();
  if (credentialID === "") {
    throw new Error(`must_change_password seed matched no active credential for ${loginId}`);
  }
  return `seeded must_change_password=true directly on Postgres for credential ${credentialID}`;
}

// assertMustChangePasswordLogin is Flow C step 2: a plain login with the
// still-current password against the seeded row must return
// must_change_password (202), never a session — identity_local.go checks
// MustChangePassword right after the password compare, before any MFA gate.
export async function assertMustChangePasswordLogin(
  ctx: LocalFlowContext,
  member: LocalMemberCredential,
): Promise<string> {
  const { context, page } = await openFreshMemberPage(ctx);
  try {
    const result = await apiFetchInPage(page, "POST", "/api/v0/auth/local/login", {
      login_id: member.loginId,
      password: member.password,
    });
    await assertNoSessionResult(page, result, 202, "must_change_password");
    return `must_change_password member login correctly returned 202 must_change_password, no session issued: ${result.text}`;
  } finally {
    await context.close();
  }
}

// assertRotateClearsMustChangeAndRecoveryLoginWorks is Flow C steps 3-4:
// rotate re-proves current_password + the member's recovery factor
// (HasActiveMFA — identity_local_rotate.go), clears must_change_password,
// and issues a session; the follow-up fresh login then proves both the NEW
// password works AND, via the SECOND still-unused recovery code, that the
// recovery-code login path remains intact after the seed/rotate detour.
export async function assertRotateClearsMustChangeAndRecoveryLoginWorks(
  ctx: LocalFlowContext,
  member: LocalMemberFlowCHandle,
  newPassword: string,
): Promise<string> {
  const rotatePath = "/api/v0/auth/local/password/rotate";
  const rotateSession = await openFreshMemberPage(ctx);
  try {
    const rotate = await apiFetchInPage(rotateSession.page, "POST", rotatePath, {
      login_id: member.loginId,
      current_password: member.password,
      recovery_code: member.recoveryCodes[0],
      new_password: newPassword,
    });
    await assertSessionIssuedResult(rotateSession.page, rotate, "from password rotation");
  } finally {
    await rotateSession.context.close();
  }

  const loginPath = "/api/v0/auth/local/login";
  const reloginSession = await openFreshMemberPage(ctx);
  try {
    const relogin = await apiFetchInPage(reloginSession.page, "POST", loginPath, {
      login_id: member.loginId,
      password: newPassword,
      recovery_code: member.recoveryCodes[1],
    });
    await assertSessionIssuedResult(reloginSession.page, relogin, "with new password + recovery");
    return (
      "rotation cleared must_change_password, issued a session; a subsequent login with the new " +
      "password + a second, still-unused recovery code also authenticated — recovery-code login unaffected"
    );
  } finally {
    await reloginSession.context.close();
  }
}
