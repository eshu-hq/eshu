// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// requireSSODecision evaluates the tenant sign-in policy's require_sso
// setting against an already-password-verified local identity (issue #4968,
// epic #4962). It is called ONLY after AuthenticateLocalIdentity has already
// verified the password (and MFA, for an admin) — this function never
// authenticates anyone; it only decides whether an already-proven identity
// is allowed to receive a session. Since issue #5001 (P2 review finding,
// PR #5049), handleLogin also calls this for a password-verified, MFA-PENDING
// non-admin (LocalIdentityAuthMFARequired, not yet Authenticated) to enforce
// require_sso precedence: such a non-admin can never complete the pending MFA
// challenge through local login when require_sso is also on, so the correct
// response is the same 403 this function already drives for an authenticated
// non-admin, not an mfa_required invitation to attempt a login that can never
// succeed. The auth.AllScopes / auth.TenantID fields this function reads are
// populated identically in both cases.
//
// Decision values (also used as the eshu_dp_auth_require_sso_login_gate_total
// "decision" label):
//   - "not_required": the tenant has no require_sso policy, or SignInPolicy
//     is unwired (fail-open). The normal, unrestricted case.
//   - "allowed_admin": require_sso is on and the identity is an admin
//     (AllScopes). This is the break-glass path — /login?local=1 is a
//     console-only UI hint to render the local form; the server applies this
//     identical rule whether or not that query parameter was present, so
//     there is no client-suppliable field that changes the outcome.
//   - "denied_non_admin": require_sso is on and the identity is not an
//     admin. The local session is not issued.
//   - "policy_read_error_admin_allowed": the policy read itself failed and
//     the already-authenticated identity is an admin (AllScopes). Admin
//     break-glass is granted regardless of the require_sso setting even when
//     it CAN be read (see "allowed_admin" above), so a read outage changes
//     nothing for this identity: it is not the fail-open path, just the
//     ordinary break-glass rule applied without first confirming the
//     (irrelevant, for an admin) policy value.
//   - "policy_read_error_fail_closed_non_admin": the policy read itself
//     failed and the identity is not an admin. Failing CLOSED here (not
//     open) is intentional: a non-admin's session depends entirely on
//     whether require_sso is off, and a read failure means that cannot be
//     confirmed. Failing open would let a non-admin log in on a tenant that
//     actually has require_sso=true during exactly the outage window an
//     attacker would want. This does not touch admin break-glass, which is
//     AllScopes-gated and unconditional (see "policy_read_error_admin_allowed"
//     above) — the original "lock out every local login including
//     break-glass" concern does not apply. The error is logged so an
//     operator sees the gap.
func (h *LocalIdentityHandler) requireSSODecision(ctx context.Context, auth LocalIdentityAuthContext) (allowed bool, decision string) {
	if h.SignInPolicy == nil {
		return true, "not_required"
	}
	policy, err := h.SignInPolicy.GetSignInPolicy(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "sign-in policy read failed during local login gate", "err", err)
		if auth.AllScopes {
			return true, "policy_read_error_admin_allowed"
		}
		return false, "policy_read_error_fail_closed_non_admin"
	}
	if !policy.RequireSSO {
		return true, "not_required"
	}
	if auth.AllScopes {
		return true, "allowed_admin"
	}
	return false, "denied_non_admin"
}

// allowLocalUserCreation evaluates the tenant sign-in policy's
// allow_local_user_creation setting for a new invitation (issue #4968). A
// nil SignInPolicy or empty tenantID fails open, and a policy read error
// fails open (logged) rather than blocking every invitation on a transient
// read failure — this gate is a provisioning control, not the require_sso
// lockout-prevention guardrail, so open failure is the lower-risk default.
func (h *LocalIdentityHandler) allowLocalUserCreation(ctx context.Context, tenantID string) bool {
	if h.SignInPolicy == nil || tenantID == "" {
		return true
	}
	policy, err := h.SignInPolicy.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		slog.ErrorContext(ctx, "sign-in policy read failed during invitation-create gate; failing open", "err", err)
		return true
	}
	return policy.AllowLocalUserCreation
}

// recordRequireSSOLoginGate increments AuthRequireSSOLoginGateTotal, the OTEL
// signal on the require_sso login-enforcement path. A nil h.Instruments is a
// no-op.
func (h *LocalIdentityHandler) recordRequireSSOLoginGate(ctx context.Context, decision string) {
	if h == nil || h.Instruments == nil || h.Instruments.AuthRequireSSOLoginGateTotal == nil {
		return
	}
	h.Instruments.AuthRequireSSOLoginGateTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionDecision, decision),
	))
}
