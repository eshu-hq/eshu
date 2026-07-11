// api/totpEnrollment.ts — self-service authenticator-app (TOTP) MFA
// enrollment (issue #4986). Two-step flow mirroring the backend:
//   1. beginTOTPEnrollment: server generates+seals a secret, returns it
//      ONCE as an otpauth:// URI (for the console to render as a QR/manual
//      key) and a factor_id.
//   2. confirmTOTPEnrollment: caller submits the first code the
//      authenticator app produced; the factor activates only on a verified
//      match.
// No secret is ever re-fetchable after step 1 — the caller must capture it
// from that one response.
import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";

// TOTPBeginResult is the view model for POST /api/v0/auth/local/mfa/totp/begin.
// Shape matches go/internal/query/local_identity_totp.go localIdentityTOTPBeginResponse.
export interface TOTPBeginResult {
  readonly factor_id: string;
  readonly otpauth_uri: string;
  readonly secret: string;
  readonly issuer: string;
  readonly digits: number;
  readonly period_seconds: number;
}

// beginTOTPEnrollment starts enrollment for the caller's own session.
// accountLabel is a cosmetic hint only (the server never has the caller's
// original login identifier — sessions carry only a one-way subject hash).
export async function beginTOTPEnrollment(
  client: EshuApiClient,
  accountLabel?: string,
): Promise<TOTPBeginResult> {
  const body =
    accountLabel && accountLabel.trim().length > 0 ? { account_label: accountLabel.trim() } : {};
  return client.postJson<TOTPBeginResult>("/api/v0/auth/local/mfa/totp/begin", body);
}

// ConfirmTOTPEnrollmentResult is a discriminated union: "activated" on
// success, "invalid_code" when the pending factor exists but the code did
// not verify (the caller may retry), and "error" for anything else
// (network, 5xx, expired/missing pending enrollment) — the message is
// generic since the backend does not distinguish those cases either.
export type ConfirmTOTPEnrollmentResult =
  | { readonly status: "activated" }
  | { readonly status: "invalid_code" }
  | { readonly status: "error"; readonly message: string };

// confirmTOTPEnrollment verifies the first submitted code and activates the
// pending factor on match.
export async function confirmTOTPEnrollment(
  client: EshuApiClient,
  factorId: string,
  code: string,
): Promise<ConfirmTOTPEnrollmentResult> {
  try {
    // Confirm returns HTTP 204 (no body) on success — postNoContent, not
    // postJson, mirrors the token-revoke route's client wrapper for the
    // same reason (client.ts: parseJson's response.json() throws on an
    // empty body).
    await client.postNoContent("/api/v0/auth/local/mfa/totp/confirm", {
      factor_id: factorId,
      code: code.trim(),
    });
    return { status: "activated" };
  } catch (e) {
    if (e instanceof EshuApiHttpError && e.status === 400) {
      return { status: "invalid_code" };
    }
    return {
      status: "error",
      message: e instanceof Error ? e.message : "Enrollment confirmation failed.",
    };
  }
}
