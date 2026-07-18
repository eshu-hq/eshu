// api/userProfile.ts
// Loaders for the profile, sessions, and token list endpoints added in Slice B
// of issue #3462. Each loader mirrors the capabilityCatalog.ts pattern: typed
// view models, explicit "unavailable" provenance on error, and NO fabricated
// rows — an error always produces an empty result set, never invented data.
//
// createPersonalApiToken / rotatePersonalApiToken / revokeApiToken (issue
// #5164) are the self-service token mutators. All three call the SAME
// all-scope-gated endpoints AdminTokensPanel already used
// (go/internal/query/local_identity_api_tokens.go); a personal-token create
// with no user_id now resolves to the caller's own identity server-side
// (see resolveSelfServiceAPITokenUserID), so this works end-to-end for the
// fresh local owner/admin console session. A non-admin session still gets a
// clean "forbidden" result — see the CreateTokenResult/"forbidden" case —
// because the create/rotate/revoke routes require all-scope auth; true
// non-admin self-service is a separate, unresolved scope question (see PR
// description).
import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

// ---------------------------------------------------------------------------
// Profile
// ---------------------------------------------------------------------------

export interface ProfileMFA {
  readonly has_active_mfa: boolean;
  readonly factor_kind?: string;
}

export interface ProfileMembership {
  readonly tenant_id: string;
  readonly workspace_id: string;
}

// ProfileData is the view model for GET /api/v0/auth/profile.
export interface ProfileData {
  readonly external_provider_config_id?: string;
  readonly active_tenant_id?: string;
  readonly active_workspace_id?: string;
  readonly role_ids?: readonly string[];
  readonly allowed_permission_features?: readonly string[];
  readonly permission_catalog_enforced: boolean;
  readonly mfa: ProfileMFA;
  readonly memberships: readonly ProfileMembership[];
}

export interface ProfileResult {
  readonly data: ProfileData | null;
  readonly provenance: "live" | "unavailable";
}

// loadProfile fetches GET /api/v0/auth/profile. On any error it returns
// provenance "unavailable" with null data — never fabricated fields.
export async function loadProfile(client: EshuApiClient): Promise<ProfileResult> {
  try {
    const data = await client.getJson<ProfileData>("/api/v0/auth/profile");
    return { data, provenance: "live" };
  } catch (err) {
    console.error("[userProfile] loadProfile failed", err);
    return { data: null, provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// BrowserSessionItem is the view model for one row from
// GET /api/v0/auth/sessions. It never includes session_hash or csrf tokens.
export interface BrowserSessionItem {
  readonly issued_at: string;
  readonly last_seen_at: string;
  readonly idle_expires_at: string;
  readonly absolute_expires_at: string;
  readonly tenant_id?: string;
  readonly workspace_id?: string;
  readonly current: boolean;
  readonly revoked_at?: string;
}

export interface SessionsResult {
  readonly sessions: readonly BrowserSessionItem[];
  readonly provenance: "live" | "unavailable";
}

interface SessionsWireResponse {
  readonly sessions?: readonly BrowserSessionItem[];
}

// loadSessions fetches GET /api/v0/auth/sessions. On any error it returns
// provenance "unavailable" with an empty array — never fabricated rows.
export async function loadSessions(client: EshuApiClient): Promise<SessionsResult> {
  try {
    const resp = await client.getJson<SessionsWireResponse>("/api/v0/auth/sessions");
    return { sessions: resp.sessions ?? [], provenance: "live" };
  } catch (err) {
    console.error("[userProfile] loadSessions failed", err);
    return { sessions: [], provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// API Tokens
// ---------------------------------------------------------------------------

// APITokenItem is the view model for one row from
// GET /api/v0/auth/local/api-tokens. It never includes token_hash or the
// display-label hash. display_label (issue #3708) is the real, non-secret
// operator-facing label persisted alongside the token and is safe to render
// verbatim; it is present only when the token was created with one.
export interface APITokenItem {
  readonly token_id: string;
  readonly token_class?: string;
  readonly display_label?: string;
  readonly issued_at: string;
  readonly expires_at?: string;
  readonly revoked_at?: string;
}

export interface TokensResult {
  readonly tokens: readonly APITokenItem[];
  readonly provenance: "live" | "unavailable";
}

interface TokensWireResponse {
  readonly tokens?: readonly APITokenItem[];
}

// loadTokens fetches GET /api/v0/auth/local/api-tokens. On any error it
// returns provenance "unavailable" with an empty array — never fabricated rows.
export async function loadTokens(client: EshuApiClient): Promise<TokensResult> {
  try {
    const resp = await client.getJson<TokensWireResponse>("/api/v0/auth/local/api-tokens");
    return { tokens: resp.tokens ?? [], provenance: "live" };
  } catch (err) {
    console.error("[userProfile] loadTokens failed", err);
    return { tokens: [], provenance: "unavailable" };
  }
}

// CreatedAPIToken is the view model for the one-time raw-token response from
// POST .../api-tokens and POST .../api-tokens/{id}/rotate. api_token is
// shown to the caller exactly once by the panel that receives this value —
// it is never re-fetchable and this module never persists it anywhere.
export interface CreatedAPIToken {
  readonly token_id: string;
  readonly api_token: string;
  readonly issued_at: string;
  readonly expires_at?: string;
}

// CreateTokenResult is a discriminated union so the panel can render an
// honest, specific outcome instead of a generic failure: "created" carries
// the one-time raw token, "forbidden" is an expected authorization signal
// (the caller's session is not all-scope/admin — not a bug), and "error"
// covers anything else (network, 5xx, validation).
export type CreateTokenResult =
  | { readonly status: "created"; readonly token: CreatedAPIToken }
  | { readonly status: "forbidden" }
  | { readonly status: "error"; readonly message: string };

// createPersonalApiToken issues a new personal token owned by the caller's
// own session (POST /api/v0/auth/local/api-tokens, token_class=personal,
// user_id omitted so the backend resolves it from the session).
export async function createPersonalApiToken(
  client: EshuApiClient,
  input: { readonly displayLabel: string; readonly expiresAt?: string },
): Promise<CreateTokenResult> {
  const body: Record<string, string> = {
    token_class: "personal",
    display_label: input.displayLabel.trim(),
  };
  if (input.expiresAt && input.expiresAt.length > 0) {
    body.expires_at = input.expiresAt;
  }
  try {
    const token = await client.postJson<CreatedAPIToken>("/api/v0/auth/local/api-tokens", body);
    return { status: "created", token };
  } catch (err) {
    if (err instanceof EshuApiHttpError && err.status === 403) {
      return { status: "forbidden" };
    }
    console.error("[userProfile] createPersonalApiToken failed", err);
    return {
      status: "error",
      message: err instanceof Error ? err.message : "Failed to create API token.",
    };
  }
}

// rotatePersonalApiToken atomically replaces tokenId with a fresh secret
// (POST /api/v0/auth/local/api-tokens/{token_id}/rotate). The replacement
// keeps the old token's owner, class, and display_label — the caller does
// not resupply them.
export async function rotatePersonalApiToken(
  client: EshuApiClient,
  tokenId: string,
): Promise<CreateTokenResult> {
  try {
    const token = await client.postJson<CreatedAPIToken>(
      `/api/v0/auth/local/api-tokens/${encodeURIComponent(tokenId)}/rotate`,
      {},
    );
    return { status: "created", token };
  } catch (err) {
    if (err instanceof EshuApiHttpError && err.status === 403) {
      return { status: "forbidden" };
    }
    console.error("[userProfile] rotatePersonalApiToken failed", err);
    return {
      status: "error",
      message: err instanceof Error ? err.message : "Failed to rotate API token.",
    };
  }
}

// revokeApiToken immediately revokes an active token
// (POST /api/v0/auth/local/api-tokens/{token_id}/revoke, HTTP 204). Mirrors
// adminConsole.ts revokeApiToken's postNoContent usage for the same route.
export async function revokeApiToken(client: EshuApiClient, tokenId: string): Promise<boolean> {
  try {
    await client.postNoContent(
      `/api/v0/auth/local/api-tokens/${encodeURIComponent(tokenId)}/revoke`,
      {},
    );
    return true;
  } catch (err) {
    console.error("[userProfile] revokeApiToken failed", err);
    return false;
  }
}
