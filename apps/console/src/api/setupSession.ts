// setupSession.ts — pure helpers for the first-run setup wizard (#4965).
// Mirrors authSession.ts's conventions: no DOM side-effects, discriminated
// result unions instead of throwing on expected auth outcomes, exact backend
// field names documented per call. Backend routes and shapes:
// go/internal/query/setup_handler.go, setup_mfa_handler.go, setup_types.go.
import type { BrowserSessionResponse, EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

// SetupStateResponse mirrors go/internal/query/setup_types.go SetupStateResponse.
export interface SetupStateResponse {
  readonly needs_setup: boolean;
  readonly bootstrap_mode: string;
}

// getSetupState fetches the public, pre-auth GET /api/v0/auth/setup-state.
// Callers should treat a fetch failure as "assume no setup needed" (fail
// toward the existing login surface, never toward an unexpected wizard).
export async function getSetupState(client: EshuApiClient): Promise<SetupStateResponse> {
  return client.getJson<SetupStateResponse>("/api/v0/auth/setup-state");
}

// SetupCredentialOptions carries the generated one-time bootstrap credential
// (or, for later steps, the reproof of it — every mutating setup route
// re-verifies on every call, see setup_handler.go's doc comment).
export interface SetupCredentialOptions {
  readonly username: string;
  readonly password: string;
}

// ClaimSetupResult is a discriminated union of every claim outcome.
//   200 → {status:"claimed"}
//   401 → invalid/expired credential
//   410 → setup is no longer available (an identity already exists)
export type ClaimSetupResult =
  | { readonly status: "claimed" }
  | { readonly status: "invalid" }
  | { readonly status: "gone" };

// claimSetup verifies the bootstrap credential without mutating any state
// (wizard step 1).
export async function claimSetup(
  client: EshuApiClient,
  opts: SetupCredentialOptions,
): Promise<ClaimSetupResult> {
  try {
    await client.postJson("/api/v0/auth/setup/claim", {
      username: opts.username,
      password: opts.password,
    });
    return { status: "claimed" };
  } catch (e) {
    return { status: mapSetupHttpError(e) };
  }
}

// CreateSetupAdminOptions reproves the bootstrap credential and supplies the
// new password that replaces it.
export interface CreateSetupAdminOptions extends SetupCredentialOptions {
  readonly newPassword: string;
}

export type CreateSetupAdminResult =
  | { readonly status: "admin_created"; readonly tenantId: string; readonly workspaceId: string }
  | { readonly status: "invalid" }
  | { readonly status: "gone" };

// createSetupAdmin replaces the bootstrap password with the operator's own
// choice (wizard step 2). Field names: go/internal/query/setup_requests.go
// setupAdminRequest (username, password, new_password).
export async function createSetupAdmin(
  client: EshuApiClient,
  opts: CreateSetupAdminOptions,
): Promise<CreateSetupAdminResult> {
  try {
    const raw = await client.postJson<{ status: string; tenant_id: string; workspace_id: string }>(
      "/api/v0/auth/setup/admin",
      { username: opts.username, password: opts.password, new_password: opts.newPassword },
    );
    return { status: "admin_created", tenantId: raw.tenant_id, workspaceId: raw.workspace_id };
  } catch (e) {
    return { status: mapSetupHttpError(e) };
  }
}

// CompleteSetupMFAResult carries the one-time plaintext recovery codes and a
// BrowserSessionResponse-shaped session so callers can reuse the same
// onSuccess(session) handler LoginPage already uses (App.tsx
// handleLoginSuccess) — wizard completion logs the operator straight in.
export type CompleteSetupMFAResult =
  | {
      readonly status: "completed";
      readonly recoveryCodes: readonly string[];
      readonly session: BrowserSessionResponse;
    }
  | { readonly status: "invalid" }
  | { readonly status: "gone" };

interface SetupCompleteRawResponse {
  readonly status: string;
  readonly recovery_codes: readonly string[];
  readonly auth: BrowserSessionResponse["auth"];
  readonly csrf_token?: string;
  readonly idle_expires_at?: string;
  readonly absolute_expires_at?: string;
}

// completeSetupMFA enrolls recovery codes and permanently seals the wizard
// (wizard step 3). The console must render recoveryCodes with copy-all and
// download, and must not call onSuccess/navigate away until the operator
// confirms they saved them — see SetupPage.tsx.
export async function completeSetupMFA(
  client: EshuApiClient,
  opts: SetupCredentialOptions,
): Promise<CompleteSetupMFAResult> {
  try {
    const raw = await client.postJson<SetupCompleteRawResponse>("/api/v0/auth/setup/mfa", {
      username: opts.username,
      password: opts.password,
    });
    return {
      status: "completed",
      recoveryCodes: raw.recovery_codes,
      session: { auth: raw.auth },
    };
  } catch (e) {
    return { status: mapSetupHttpError(e) };
  }
}

// mapSetupHttpError maps the two expected setup-route HTTP outcomes to their
// discriminated status strings and rethrows everything else (5xx, network,
// timeout) — never swallowed, matching authSession.ts's loginLocal.
function mapSetupHttpError(e: unknown): "invalid" | "gone" {
  if (e instanceof EshuApiHttpError) {
    if (e.status === 401) return "invalid";
    if (e.status === 410) return "gone";
  }
  throw e;
}
