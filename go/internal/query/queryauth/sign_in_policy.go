// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"context"
	"time"
)

// SignInPolicy is the tenant sign-in policy (issue #4968). A tenant with no
// configured policy reads as RequireSSO=false, AllowLocalUserCreation=true,
// RequireMFAForAllUsers=false, and zero-value ("use the process default")
// timeouts. Moved from root package query's sign_in_policy_types.go (#6642)
// so a handler-family subpackage can read a tenant's sign-in policy without
// importing root. Root's write-side types (SignInPolicyUpdateRequest,
// SignInPolicyMutationStore) and guardrail sentinel errors stay in root: no
// hoisted symbol needs them.
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

// SignInPolicyReadStore is the read surface for tenant sign-in policy.
// Moved alongside SignInPolicy.
type SignInPolicyReadStore interface {
	GetSignInPolicy(ctx context.Context, tenantID string) (SignInPolicy, error)
}
