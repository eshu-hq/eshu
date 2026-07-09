// api/adminSignInPolicy.ts
// Loaders and mutators for the tenant sign-in policy surface (#4968, epic
// #4962) consumed by the Admin -> Identity & Access -> Sign-in policy tab.
// Field names mirror the backend OpenAPI fragments verbatim
// (go/internal/query/openapi_paths_auth_sign_in_policy.go,
//  openapi_components_sign_in_policy.go). This module never models a
// secret — sso_admin_verified_provider_config_id is an operator-assigned
// config id, not a credential, matching the backend's own response shape
// (go/internal/query/sign_in_policy_reads.go signInPolicyDetailJSON).
import type { AdminProvenance } from "./adminConsoleTypes";
import type { EshuApiClient } from "./client";

const ADMIN_SIGN_IN_POLICY_PATH = "/api/v0/auth/admin/sign-in-policy";
const PUBLIC_SIGN_IN_POLICY_PATH = "/api/v0/auth/sign-in-policy";

export interface AdminSignInPolicy {
  readonly tenant_id: string;
  readonly require_sso: boolean;
  readonly allow_local_user_creation: boolean;
  readonly require_mfa_for_all_users: boolean;
  readonly idle_timeout_seconds: number;
  readonly absolute_timeout_seconds: number;
  readonly sso_admin_verified_at?: string;
  readonly sso_admin_verified_provider_config_id?: string;
  readonly policy_revision_hash: string;
  readonly updated_at: string;
}

export interface AdminSignInPolicyResult {
  readonly policy?: AdminSignInPolicy;
  readonly provenance: AdminProvenance;
}

// loadAdminSignInPolicy — GET /api/v0/auth/admin/sign-in-policy. On a load
// error it returns provenance "unavailable" with no policy — never a
// fabricated policy, matching the existing admin-panel convention (see
// adminProviderConfig.ts's loadProviderConfigs).
export async function loadAdminSignInPolicy(
  client: EshuApiClient,
): Promise<AdminSignInPolicyResult> {
  try {
    const policy = await client.getJson<AdminSignInPolicy>(ADMIN_SIGN_IN_POLICY_PATH);
    return { policy, provenance: "live" };
  } catch (err) {
    console.error("[adminSignInPolicy] loadAdminSignInPolicy failed", err);
    return { provenance: "unavailable" };
  }
}

export interface SignInPolicyUpdateInput {
  readonly requireSso?: boolean;
  readonly allowLocalUserCreation?: boolean;
  readonly requireMfaForAllUsers?: boolean;
  readonly idleTimeoutSeconds?: number;
  readonly absoluteTimeoutSeconds?: number;
}

function toUpdateWireBody(input: SignInPolicyUpdateInput): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (input.requireSso !== undefined) body.require_sso = input.requireSso;
  if (input.allowLocalUserCreation !== undefined) {
    body.allow_local_user_creation = input.allowLocalUserCreation;
  }
  if (input.requireMfaForAllUsers !== undefined) {
    body.require_mfa_for_all_users = input.requireMfaForAllUsers;
  }
  if (input.idleTimeoutSeconds !== undefined) body.idle_timeout_seconds = input.idleTimeoutSeconds;
  if (input.absoluteTimeoutSeconds !== undefined) {
    body.absolute_timeout_seconds = input.absoluteTimeoutSeconds;
  }
  return body;
}

// SignInPolicyUpdateOutcome never throws to the caller — a guardrail
// rejection or any other failure is reported as ok:false with the server's
// display-safe message (the guardrail message itself, e.g. "require_sso
// cannot be enabled: no admin has signed in via SSO yet"), mirroring
// ProviderConfigWriteOutcome's "mutators return an outcome, never throw"
// convention.
export interface SignInPolicyUpdateOutcome {
  readonly ok: boolean;
  readonly policy?: AdminSignInPolicy;
  readonly errorMessage?: string;
}

// updateAdminSignInPolicy — PATCH /api/v0/auth/admin/sign-in-policy. Only
// the fields present on `input` are sent; every other field on the tenant's
// policy is left unchanged server-side.
export async function updateAdminSignInPolicy(
  client: EshuApiClient,
  input: SignInPolicyUpdateInput,
): Promise<SignInPolicyUpdateOutcome> {
  try {
    const policy = await client.patchJson<AdminSignInPolicy>(
      ADMIN_SIGN_IN_POLICY_PATH,
      toUpdateWireBody(input),
    );
    return { ok: true, policy };
  } catch (err) {
    console.error("[adminSignInPolicy] updateAdminSignInPolicy failed", err);
    return {
      ok: false,
      errorMessage: err instanceof Error ? err.message : "updateAdminSignInPolicy failed",
    };
  }
}

interface PublicSignInPolicyWire {
  readonly require_sso?: boolean;
}

// loadPublicRequireSSO — GET /api/v0/auth/sign-in-policy (public, pre-auth).
// Used by LoginPage to decide whether to hide the local password form. On
// any error, or when tenantId is empty, it returns false — the same
// fail-open default the backend's own handler applies (this is a UX hint
// only; POST /api/v0/auth/local/login is the real enforcement boundary and
// is unaffected by this value).
export async function loadPublicRequireSSO(
  client: EshuApiClient,
  tenantId: string | undefined,
): Promise<boolean> {
  if (!tenantId) return false;
  try {
    const resp = await client.getJson<PublicSignInPolicyWire>(
      `${PUBLIC_SIGN_IN_POLICY_PATH}?tenant_id=${encodeURIComponent(tenantId)}`,
    );
    return resp.require_sso ?? false;
  } catch (err) {
    console.error("[adminSignInPolicy] loadPublicRequireSSO failed", err);
    return false;
  }
}
