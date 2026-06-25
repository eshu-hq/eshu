// authSession.ts — pure helpers for browser-session lifecycle.
// No DOM side-effects; redirect callers accept an optional redirect fn so tests
// can verify the URL without triggering navigation.
import type { BrowserSessionResponse, EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

// loadCurrentSession fetches the current server-managed browser session.
// Returns null when no session exists (401/403/404). Rethrows all other errors
// so callers can distinguish "no session" from "API unavailable".
export async function loadCurrentSession(
  client: EshuApiClient,
): Promise<BrowserSessionResponse | null> {
  try {
    return await client.getBrowserSession();
  } catch (e) {
    if (
      e instanceof EshuApiHttpError &&
      (e.status === 401 || e.status === 403 || e.status === 404)
    ) {
      return null;
    }
    throw e;
  }
}

// LoginLocalOptions carries the credentials for local-identity login.
// Backend field names: login_id, password, recovery_code (optional MFA code).
export interface LoginLocalOptions {
  readonly login: string;
  readonly password: string;
  readonly mfaCode?: string;
}

// LocalLoginResult is a discriminated union of every possible local-login
// outcome. The backend always returns a JSON body even on 4xx (confirmed from
// go/internal/query/local_identity_handler_helpers.go:writeLocalIdentityUnauthenticated).
// HTTP status codes map as follows:
//   200 → { status: "ok", session }    — full BrowserSessionResponse in body
//   202 → { status: "mfa_required" }   — password accepted, MFA proof needed
//   423 → { status: "locked", lockedUntil? } — EshuApiHttpError status=423
//   403 → { status: "disabled" }       — EshuApiHttpError status=403
//   401 → { status: "invalid" }        — EshuApiHttpError status=401
// Any other error (5xx, network, timeout) is rethrown — never swallowed.
export type LocalLoginResult =
  | { readonly status: "ok"; readonly session: BrowserSessionResponse }
  | { readonly status: "mfa_required" }
  | { readonly status: "locked"; readonly lockedUntil?: string }
  | { readonly status: "disabled" }
  | { readonly status: "invalid" };

// LocalLoginRawResponse is the union of shapes the backend can return on
// POST /api/v0/auth/local/login for any 2xx status. Status 202 carries only
// the status marker; status 200 carries the full BrowserSessionResponse under
// `session`. Keeping the union narrow (instead of `any`) keeps downstream
// access typed and ESLint happy.
export type LocalLoginRawResponse =
  | { readonly status: "mfa_required" }
  | ({ readonly status: "ok" } & BrowserSessionResponse);

// loginLocal POSTs credentials to /api/v0/auth/local/login and returns a
// discriminated LocalLoginResult. Non-auth errors (5xx, network) are rethrown.
// Exact backend field names from go/internal/query/local_identity_requests.go:
//   login_id, password, recovery_code
export async function loginLocal(
  client: EshuApiClient,
  opts: LoginLocalOptions,
): Promise<LocalLoginResult> {
  const body: Record<string, string> = {
    login_id: opts.login,
    password: opts.password,
  };
  if (opts.mfaCode !== undefined && opts.mfaCode.trim().length > 0) {
    body["recovery_code"] = opts.mfaCode.trim();
  }
  try {
    // postJson resolves for 2xx. 202 (mfa_required) is 2xx, so it resolves
    // with the body {status:"mfa_required"} — not a BrowserSessionResponse.
    // The narrow `LocalLoginRawResponse` keeps the status field typed while
    // letting us cast to BrowserSessionResponse in the success branch.
    const raw = await client.postJson<LocalLoginRawResponse>("/api/v0/auth/local/login", body);
    if (raw.status === "mfa_required") {
      return { status: "mfa_required" };
    }
    // 200 authenticated — body is the BrowserSessionResponse itself; wrap it
    // for the discriminated result.
    return { status: "ok", session: raw };
  } catch (e) {
    if (e instanceof EshuApiHttpError) {
      if (e.status === 423) {
        // Body carries locked_until as ISO string but postJson threw — we cannot
        // read the response body here. Callers display generic locked message.
        return { status: "locked" };
      }
      if (e.status === 403) {
        return { status: "disabled" };
      }
      if (e.status === 401) {
        return { status: "invalid" };
      }
      // Any other HTTP error (5xx, 503, etc.) is not an auth outcome — rethrow.
      throw e;
    }
    // Network/timeout errors — rethrow.
    throw e;
  }
}

// OidcLoginOptions selects the provider and optional workspace context.
// Query param names confirmed from go/internal/query/oidc_login_handler.go:
//   provider_config_id, tenant_id, workspace_id, return_to
export interface OidcLoginOptions {
  readonly providerConfigId: string;
  readonly tenantId?: string;
  readonly workspaceId?: string;
  readonly returnTo: string;
}

// beginOidcLogin builds the OIDC login redirect URL and optionally performs
// the redirect via redirectFn (defaults to location.assign). Returns the URL
// so tests can verify it without triggering navigation.
// NOTE: No provider-discovery endpoint exists in Slice A (#3682). The UI
// hides OIDC/SAML buttons until provider discovery is wired up.
export function beginOidcLogin(
  baseUrl: string,
  opts: OidcLoginOptions,
  redirectFn?: (url: string) => void,
): string {
  const normalizedBase = baseUrl.endsWith("/") ? baseUrl.slice(0, -1) : baseUrl;
  const params = new URLSearchParams();
  params.set("provider_config_id", opts.providerConfigId);
  if (opts.tenantId) params.set("tenant_id", opts.tenantId);
  if (opts.workspaceId) params.set("workspace_id", opts.workspaceId);
  params.set("return_to", opts.returnTo);
  const url = `${normalizedBase}/api/v0/auth/oidc/login?${params.toString()}`;
  if (redirectFn) {
    redirectFn(url);
  }
  return url;
}

// SamlLoginOptions selects the SAML provider by ID.
// Path param: provider_id (from GET /api/v0/auth/saml/providers/{provider_id}/login)
// NOTE: No provider-discovery endpoint exists in Slice A (#3682). The UI
// hides OIDC/SAML buttons until provider discovery is wired up.
export interface SamlLoginOptions {
  readonly providerId: string;
  readonly returnTo: string;
}

// beginSamlLogin builds the SAML login redirect URL and optionally performs
// the redirect via redirectFn. Returns the URL for testability.
export function beginSamlLogin(
  baseUrl: string,
  opts: SamlLoginOptions,
  redirectFn?: (url: string) => void,
): string {
  const normalizedBase = baseUrl.endsWith("/") ? baseUrl.slice(0, -1) : baseUrl;
  const params = new URLSearchParams();
  params.set("return_to", opts.returnTo);
  const url = `${normalizedBase}/api/v0/auth/saml/providers/${encodeURIComponent(opts.providerId)}/login?${params.toString()}`;
  if (redirectFn) {
    redirectFn(url);
  }
  return url;
}

// AuthLoginProvider is the pre-auth view of one configured SSO provider.
// Only the opaque provider_config_id, a safe generic display_label, and the
// protocol class (provider_kind) are ever returned — no secrets, metadata URLs,
// IdP hostnames, org names, or group names. Shape matches
// go/internal/query/auth_providers_handler.go AuthProviderItem.
export interface AuthLoginProvider {
  readonly provider_config_id: string;
  readonly display_label: string;
  readonly provider_kind: "oidc" | "saml";
}

// listAuthProviders fetches the pre-auth provider discovery endpoint scoped to
// a single tenant. tenantId must be non-empty to receive a non-empty response;
// when tenantId is absent the endpoint returns an empty array (no global scan).
// Returns an empty array when no providers are configured, when tenantId is
// absent, or when the fetch fails (the login page falls back to local-only).
export async function listAuthProviders(
  client: EshuApiClient,
  tenantId?: string,
): Promise<readonly AuthLoginProvider[]> {
  try {
    const trimmed = tenantId?.trim() ?? "";
    const path =
      trimmed.length > 0
        ? `/api/v0/auth/providers?tenant_id=${encodeURIComponent(trimmed)}`
        : "/api/v0/auth/providers";
    const resp = await client.getJson<{ providers: AuthLoginProvider[] }>(path);
    return resp.providers ?? [];
  } catch (err) {
    // Pre-auth network errors must never break the login form, but warn so
    // backend outages are visible in devtools during development and triage.
    console.warn("[eshu] GET /api/v0/auth/providers failed — SSO buttons hidden", err);
    return [];
  }
}

// logout revokes the current browser session.
export async function logout(client: EshuApiClient): Promise<void> {
  await client.logoutBrowserSession();
}
