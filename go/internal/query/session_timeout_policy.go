// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// resolveSessionTimeouts resolves the idle/absolute session timeout for one
// tenant (issue #4968, epic #4962), preferring a per-tenant SignInPolicy
// override and falling back to the caller-supplied process-wide defaults
// (DefaultBrowserSessionIdleTimeout/AbsoluteTimeout, or a handler's
// explicitly configured override) when no policy store is wired, the
// tenant has no policy row, or the policy read fails.
//
// A read failure fails open to the STATIC default, not to zero or an error:
// this runs on every session-issuing call (local login, break-glass, OIDC,
// SAML), so a transient sign-in-policy outage must never change every
// session's lifetime or block login — the same fail-open convention
// requireSSODecision uses for the require_sso gate
// (local_identity_sign_in_policy_gate.go).
func resolveSessionTimeouts(
	ctx context.Context,
	signInPolicy SignInPolicyReadStore,
	tenantID string,
	defaultIdle time.Duration,
	defaultAbsolute time.Duration,
) (idle time.Duration, absolute time.Duration) {
	idle, absolute = defaultIdle, defaultAbsolute
	tenantID = strings.TrimSpace(tenantID)
	if signInPolicy == nil || tenantID == "" {
		return idle, absolute
	}
	policy, err := signInPolicy.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		slog.ErrorContext(ctx, "sign-in policy read failed while resolving session timeouts; using process default", "err", err)
		return idle, absolute
	}
	if policy.IdleTimeoutSeconds > 0 {
		idle = time.Duration(policy.IdleTimeoutSeconds) * time.Second
	}
	if policy.AbsoluteTimeoutSeconds > 0 {
		absolute = time.Duration(policy.AbsoluteTimeoutSeconds) * time.Second
	}
	// An idle timeout longer than the absolute timeout is meaningless (the
	// absolute expiry always fires first); clamp rather than reject, mirroring
	// issueBrowserSessionWithExternalAuth's existing idleExpiresAt-after-
	// absoluteExpiresAt clamp.
	if idle > absolute {
		idle = absolute
	}
	return idle, absolute
}
