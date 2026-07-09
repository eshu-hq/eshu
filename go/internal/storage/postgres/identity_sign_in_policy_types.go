// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"time"
)

// Sign-in policy sentinel errors (epic #4962, issue #4968). The guardrail
// errors are returned only from UpsertSignInPolicy when the request would
// set RequireSSO true; they never block reads or any other field update.
var (
	// ErrSignInPolicyGuardrailNoProvenProvider means the tenant has no
	// provider config with Status="active" — provider status only reaches
	// "active" through a synchronous passing TestProviderConnection call at
	// enable time (see AdminProviderConfigMutationStore.EnableProviderConfig
	// in go/internal/query), so an active provider IS the durable proof of a
	// passing connection test. require_sso cannot be enabled without one.
	ErrSignInPolicyGuardrailNoProvenProvider = errors.New("sign-in policy: require_sso needs at least one provider config with a passing connection test")
	// ErrSignInPolicyGuardrailNoSSOAdminProof means no admin has ever
	// completed an SSO sign-in for this tenant (sso_admin_verified_at is
	// unset). require_sso cannot be enabled until that has happened at least
	// once, so a dead or misconfigured IdP can never lock the tenant out.
	ErrSignInPolicyGuardrailNoSSOAdminProof = errors.New("sign-in policy: require_sso needs at least one admin to have signed in via SSO")
)

// SignInPolicy is the tenant sign-in policy row. A tenant with no row yet
// reads as the zero-configuration default: RequireSSO=false,
// AllowLocalUserCreation=true, RequireMFAForAllUsers=false, and zero-value
// (meaning "use the process default") timeouts.
type SignInPolicy struct {
	TenantID               string
	RequireSSO             bool
	AllowLocalUserCreation bool
	RequireMFAForAllUsers  bool
	// IdleTimeoutSeconds/AbsoluteTimeoutSeconds are 0 when unset, meaning the
	// caller should fall back to DefaultBrowserSessionIdleTimeout /
	// DefaultBrowserSessionAbsoluteTimeout (go/internal/query).
	IdleTimeoutSeconds               int
	AbsoluteTimeoutSeconds           int
	SSOAdminVerifiedAt               time.Time
	SSOAdminVerifiedProviderConfigID string
	PolicyRevisionHash               string
	UpdatedAt                        time.Time
}

// SignInPolicyUpdate carries a partial update to one tenant's sign-in
// policy. A nil field is left unchanged; a non-nil field is applied.
// PolicyRevisionHash and Now are always required.
type SignInPolicyUpdate struct {
	RequireSSO             *bool
	AllowLocalUserCreation *bool
	RequireMFAForAllUsers  *bool
	IdleTimeoutSeconds     *int
	AbsoluteTimeoutSeconds *int
	PolicyRevisionHash     string
	Now                    time.Time
}

// defaultSignInPolicy returns the zero-configuration policy for a tenant with
// no persisted row.
func defaultSignInPolicy(tenantID string) SignInPolicy {
	return SignInPolicy{
		TenantID:               tenantID,
		RequireSSO:             false,
		AllowLocalUserCreation: true,
		RequireMFAForAllUsers:  false,
	}
}
