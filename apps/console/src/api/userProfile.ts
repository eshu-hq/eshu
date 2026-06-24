// api/userProfile.ts
// Loaders for the profile, sessions, and token list endpoints added in Slice B
// of issue #3462. Each loader mirrors the capabilityCatalog.ts pattern: typed
// view models, explicit "unavailable" provenance on error, and NO fabricated
// rows — an error always produces an empty result set, never invented data.
import type { EshuApiClient } from "./client";

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
  } catch {
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
  } catch {
    return { sessions: [], provenance: "unavailable" };
  }
}

// ---------------------------------------------------------------------------
// API Tokens
// ---------------------------------------------------------------------------

// APITokenItem is the view model for one row from
// GET /api/v0/auth/local/api-tokens. It never includes token_hash.
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
  } catch {
    return { tokens: [], provenance: "unavailable" };
  }
}
