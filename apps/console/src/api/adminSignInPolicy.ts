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

// SignInPolicyUpdateWireBody is the exact JSON shape
// signInPolicyUpdateRequestBody expects (go/internal/query/
// sign_in_policy_mutations.go). A typed field set (not
// Record<string, unknown>) so a field-name typo fails at compile time,
// matching adminProviderConfig.ts's toWireBody convention.
interface SignInPolicyUpdateWireBody {
  readonly require_sso?: boolean;
  readonly allow_local_user_creation?: boolean;
  readonly require_mfa_for_all_users?: boolean;
  readonly idle_timeout_seconds?: number;
  readonly absolute_timeout_seconds?: number;
}

function toUpdateWireBody(input: SignInPolicyUpdateInput): SignInPolicyUpdateWireBody {
  return {
    ...(input.requireSso !== undefined ? { require_sso: input.requireSso } : {}),
    ...(input.allowLocalUserCreation !== undefined
      ? { allow_local_user_creation: input.allowLocalUserCreation }
      : {}),
    ...(input.requireMfaForAllUsers !== undefined
      ? { require_mfa_for_all_users: input.requireMfaForAllUsers }
      : {}),
    ...(input.idleTimeoutSeconds !== undefined
      ? { idle_timeout_seconds: input.idleTimeoutSeconds }
      : {}),
    ...(input.absoluteTimeoutSeconds !== undefined
      ? { absolute_timeout_seconds: input.absoluteTimeoutSeconds }
      : {}),
  };
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
