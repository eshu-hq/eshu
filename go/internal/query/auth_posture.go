// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
)

// AuthPosture is the derived pre-auth sign-in posture for one tenant: which
// providers are enabled, whether the local password form is offered, and
// whether self-service personal API tokens are offered. It answers "what is
// this org's auth posture" (issue #5165, F-4) from a single reusable
// derivation (DeriveAuthPosture) rather than each surface reimplementing the
// question. GET /api/v0/auth/providers (AuthProviderListHandler) serves this
// shape to the console login picker, and issue #5163 (F-2)'s MCP OAuth
// discovery route consumes the SAME derivation to decide whether the
// protected-resource-metadata route is mounted at all — do not duplicate
// this logic at another call site; import and call DeriveAuthPosture.
type AuthPosture struct {
	// Providers is the tenant's configured, login-facing OIDC/SAML
	// providers. Always a non-nil slice (possibly empty), never nil, so
	// JSON encodes it as `[]` rather than `null`.
	Providers []AuthProviderItem `json:"providers"`
	// LocalLoginOffered reports whether the local username/password form
	// should be shown. It is a UX hint only, mirroring the console's
	// existing showLocalForm rule (LoginPage.tsx): false exactly when the
	// tenant's sign-in policy has require_sso=true. It is NEVER the real
	// authorization boundary — POST /api/v0/auth/local/login enforces the
	// identical break-glass-admin-only rule via requireSSODecision
	// (local_identity_sign_in_policy_gate.go) regardless of this value.
	LocalLoginOffered bool `json:"local_login_offered"`
	// SelfServiceTokensOffered reports whether an authenticated caller may
	// self-issue a personal API token (issue #5164). It is always true
	// today: self-service token creation has no sign-in-policy gate. The
	// field exists so a future policy toggle can flip it without changing
	// this shape or any of its consumers.
	SelfServiceTokensOffered bool `json:"self_service_tokens_offered"`
}

// DeriveAuthPosture computes the pre-auth sign-in posture for tenantID from
// the tenant's configured login providers and its sign-in policy. This is
// the single reusable derivation issue #5165 (F-4) requires and issue #5163
// (F-2) consumes unchanged for its MCP OAuth-discovery enablement check
// ("provider rows + sign-in policy — same posture as F-4"): F-2 calls this
// same function with the same providers/policy stores rather than
// reimplementing the enabled-provider question.
//
// tenantID must be non-empty to receive anything but the safe
// zero-configuration default (empty provider list, local login offered,
// self-service tokens offered) — an empty tenantID never triggers a global
// cross-tenant scan on either store, matching
// AuthProviderListHandler.handleList and SignInPolicyReadHandler.
// handlePublicGet's existing empty-tenant_id behavior.
//
// A nil providers store returns the same safe default. A nil policy store
// defaults LocalLoginOffered to true (matching AuthProviderListHandler's
// nil-safe convention for a store not wired in a given environment).
//
// A provider-list read failure propagates to the caller: the provider list
// is the primary discovery signal, so callers must surface it as an error
// (matching AuthProviderListHandler's pre-existing 500 on a
// ListLoginProviders error) rather than silently render an empty picker. A
// sign-in-policy read failure instead fails OPEN (LocalLoginOffered stays
// true, logged) — this mirrors SignInPolicyReadHandler.handlePublicGet's own
// fail-open default for the identical field, since it is a UX hint whose
// real enforcement lives entirely in requireSSODecision, unaffected by a
// transient read outage here.
func DeriveAuthPosture(
	ctx context.Context,
	providers AuthProviderStore,
	policy SignInPolicyReadStore,
	tenantID string,
) (AuthPosture, error) {
	posture := AuthPosture{
		Providers:                []AuthProviderItem{},
		LocalLoginOffered:        true,
		SelfServiceTokensOffered: true,
	}
	if tenantID == "" || providers == nil {
		return posture, nil
	}

	items, err := providers.ListLoginProviders(ctx, tenantID)
	if err != nil {
		return AuthPosture{}, err
	}
	if items != nil {
		posture.Providers = items
	}

	if policy != nil {
		signInPolicy, err := policy.GetSignInPolicy(ctx, tenantID)
		if err != nil {
			slog.ErrorContext(ctx, "auth posture: sign-in policy read failed; local login stays offered (fail open)", "err", err)
		} else {
			posture.LocalLoginOffered = !signInPolicy.RequireSSO
		}
	}

	return posture, nil
}
