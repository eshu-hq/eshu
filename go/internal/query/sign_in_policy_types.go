// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"time"
)

// Sign-in policy guardrail sentinel errors (epic #4962, issue #4968). An
// AdminSignInPolicyMutationStore implementation (in cmd/api, backed by
// storage/postgres) returns these so this handler can map them to a 400
// guardrail response without importing storage/postgres directly.
var (
	// ErrSignInPolicyGuardrailNoProvenProvider mirrors
	// postgres.ErrSignInPolicyGuardrailNoProvenProvider: the tenant has no
	// provider config with a passing connection test.
	ErrSignInPolicyGuardrailNoProvenProvider = errors.New("sign-in policy: require_sso needs at least one provider config with a passing connection test")
	// ErrSignInPolicyGuardrailNoSSOAdminProof mirrors
	// postgres.ErrSignInPolicyGuardrailNoSSOAdminProof: no admin has ever
	// completed an SSO sign-in for this tenant.
	ErrSignInPolicyGuardrailNoSSOAdminProof = errors.New("sign-in policy: require_sso needs at least one admin to have signed in via SSO")
	// ErrSignInPolicyTimeoutOrdering mirrors
	// postgres.ErrSignInPolicyTimeoutOrdering: the merged (stored+incoming)
	// idle/absolute timeout pair, validated INSIDE the store's row-locked
	// transaction, would leave a non-zero absolute_timeout_seconds shorter
	// than a non-zero idle_timeout_seconds (issue #5002 part 2).
	ErrSignInPolicyTimeoutOrdering = errors.New("sign-in policy: absolute_timeout_seconds must not be less than idle_timeout_seconds")
)

// SignInPolicy is the tenant sign-in policy (issue #4968). A tenant with no
// configured policy reads as RequireSSO=false, AllowLocalUserCreation=true,
// RequireMFAForAllUsers=false, and zero-value ("use the process default")
// timeouts.
type SignInPolicy struct {
	TenantID                         string
	RequireSSO                       bool
	AllowLocalUserCreation           bool
	RequireMFAForAllUsers            bool
	IdleTimeoutSeconds               int
	AbsoluteTimeoutSeconds           int
	SSOAdminVerifiedAt               time.Time
	SSOAdminVerifiedProviderConfigID string
	PolicyRevisionHash               string
	UpdatedAt                        time.Time
}

// SignInPolicyUpdateRequest is a partial update to one tenant's sign-in
// policy. A nil field is left unchanged.
type SignInPolicyUpdateRequest struct {
	RequireSSO             *bool
	AllowLocalUserCreation *bool
	RequireMFAForAllUsers  *bool
	IdleTimeoutSeconds     *int
	AbsoluteTimeoutSeconds *int
}

// SignInPolicyReadStore is the read surface for tenant sign-in policy.
type SignInPolicyReadStore interface {
	GetSignInPolicy(ctx context.Context, tenantID string) (SignInPolicy, error)
}

// SignInPolicyMutationStore is the write surface for tenant sign-in policy.
// Implementations must apply the require_sso guardrail AND the merged
// idle/absolute timeout-ordering check atomically with the write, INSIDE the
// same row-locked transaction (see storage/postgres.IdentitySubjectStore.
// UpsertSignInPolicy), and return ErrSignInPolicyGuardrailNoProvenProvider /
// ErrSignInPolicyGuardrailNoSSOAdminProof / ErrSignInPolicyTimeoutOrdering
// (or an error satisfying errors.Is against the storage-layer sentinels)
// when a check blocks the write. A handler-side pre-transaction read of the
// current policy is NOT part of this contract — issue #5002 part 2 (codex
// PR #5053 review) found that racy: two concurrent partial PATCHes could
// each read the same stale value and both pass, so the ordering check must
// live under the lock this interface's implementation holds, not in the
// caller.
type SignInPolicyMutationStore interface {
	UpsertSignInPolicy(
		ctx context.Context,
		tenantID string,
		update SignInPolicyUpdateRequest,
		policyRevisionHash string,
		now time.Time,
	) (SignInPolicy, error)
}
