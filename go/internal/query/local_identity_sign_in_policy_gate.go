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
// setting against an already-authenticated local identity (issue #4968,
// epic #4962). It is called ONLY after AuthenticateLocalIdentity has already
// verified the password (and MFA, for an admin) — this function never
// authenticates anyone; it only decides whether an already-proven identity
// is allowed to receive a session.
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
//   - "policy_read_error_fail_open": the policy read itself failed. Failing
//     open here (not closed) is intentional: failing closed would mean a
//     transient sign-in-policy read error locks out EVERY local login for
//     the tenant, including admin break-glass — the exact outage break-glass
//     exists to prevent. The error is logged so an operator sees the gap.
func (h *LocalIdentityHandler) requireSSODecision(ctx context.Context, auth LocalIdentityAuthContext) (allowed bool, decision string) {
	if h.SignInPolicy == nil {
		return true, "not_required"
	}
	policy, err := h.SignInPolicy.GetSignInPolicy(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "sign-in policy read failed during local login gate; failing open", "err", err)
		return true, "policy_read_error_fail_open"
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
