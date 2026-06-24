// authSession.ts — pure helpers for browser-session lifecycle.
// No DOM side-effects; redirect callers accept an optional redirect fn so tests
// can verify the URL without triggering navigation.
import type { BrowserSessionResponse, EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

// loadCurrentSession fetches the current server-managed browser session.
// Returns null when no session exists (401/403/404). Rethrows all other errors
// so callers can distinguish "no session" from "API unavailable".
export async function loadCurrentSession(
  client: EshuApiClient
): Promise<BrowserSessionResponse | null> {
  try {
    return await client.getBrowserSession();
  } catch (e) {
    if (e instanceof EshuApiHttpError && (e.status === 401 || e.status === 403 || e.status === 404)) {
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

// loginLocal POSTs credentials to /api/v0/auth/local/login.
// Exact backend field names confirmed from go/internal/query/local_identity_requests.go:
//   login_id, password, recovery_code
// Throws EshuApiHttpError on any backend error (401, 403, etc.) — callers must
// surface errors explicitly.
export async function loginLocal(
  client: EshuApiClient,
  opts: LoginLocalOptions
): Promise<BrowserSessionResponse> {
  const body: Record<string, string> = {
    login_id: opts.login,
    password: opts.password
  };
  if (opts.mfaCode !== undefined && opts.mfaCode.trim().length > 0) {
    body["recovery_code"] = opts.mfaCode.trim();
  }
  return client.postJson<BrowserSessionResponse>("/api/v0/auth/local/login", body);
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
export function beginOidcLogin(
  baseUrl: string,
  opts: OidcLoginOptions,
  redirectFn?: (url: string) => void
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
export interface SamlLoginOptions {
  readonly providerId: string;
  readonly returnTo: string;
}

// beginSamlLogin builds the SAML login redirect URL and optionally performs
// the redirect via redirectFn. Returns the URL for testability.
export function beginSamlLogin(
  baseUrl: string,
  opts: SamlLoginOptions,
  redirectFn?: (url: string) => void
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

// logout revokes the current browser session.
export async function logout(client: EshuApiClient): Promise<void> {
  await client.logoutBrowserSession();
}
