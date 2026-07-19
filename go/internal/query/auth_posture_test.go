// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// TestDeriveAuthPostureRequireSSOHidesLocalLogin proves the require_sso=true
// case from issue #5165's acceptance criteria: an org with a provider
// configured AND require_sso on gets LocalLoginOffered=false while its
// provider list is unaffected. This is the same signal the console picker
// uses to hide the local form (LoginPage.tsx's showLocalForm).
func TestDeriveAuthPostureRequireSSOHidesLocalLogin(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant_a": {
				{ProviderConfigID: "okta-oidc", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
			},
		},
	}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}

	posture, err := DeriveAuthPosture(context.Background(), providers, policy, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = true, want false under require_sso")
	}
	if len(posture.Providers) != 1 || posture.Providers[0].ProviderConfigID != "okta-oidc" {
		t.Errorf("Providers = %#v, want the single configured okta-oidc provider", posture.Providers)
	}
	if !posture.SelfServiceTokensOffered {
		t.Error("SelfServiceTokensOffered = false, want true (no sign-in-policy gate exists for self-service tokens today)")
	}
}

// TestDeriveAuthPostureNoRequireSSOOffersLocalLogin proves the "Okta but no
// require_sso" acceptance criterion: the provider button AND the local form
// are both offered.
func TestDeriveAuthPostureNoRequireSSOOffersLocalLogin(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant_a": {
				{ProviderConfigID: "okta-oidc", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
			},
		},
	}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: false}}

	posture, err := DeriveAuthPosture(context.Background(), providers, policy, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true when require_sso is off")
	}
	if len(posture.Providers) != 1 {
		t.Errorf("Providers = %#v, want 1 configured provider", posture.Providers)
	}
}

// TestDeriveAuthPostureZeroProvidersOffersLocalLoginOnly proves the "org with
// nothing configured" acceptance criterion: empty provider list, local login
// still offered.
func TestDeriveAuthPostureZeroProvidersOffersLocalLoginOnly(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{}}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: false}}

	posture, err := DeriveAuthPosture(context.Background(), providers, policy, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if posture.Providers == nil || len(posture.Providers) != 0 {
		t.Fatalf("Providers = %#v, want empty non-nil slice", posture.Providers)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true when nothing is configured")
	}
}

// TestDeriveAuthPostureEmptyTenantIDFailsSafe proves that an unresolvable
// tenant never triggers a global cross-tenant scan and returns the safe
// zero-configuration default (matching AuthProviderListHandler's existing
// empty-tenant_id behavior), without ever calling either store.
func TestDeriveAuthPostureEmptyTenantIDFailsSafe(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant_a": {{ProviderConfigID: "okta-oidc", DisplayLabel: "x", ProviderKind: "oidc"}},
		},
	}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{RequireSSO: true}}

	posture, err := DeriveAuthPosture(context.Background(), providers, policy, "")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if len(posture.Providers) != 0 {
		t.Errorf("Providers = %#v, want empty for unresolvable tenant", posture.Providers)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true (safe default) for unresolvable tenant")
	}
	if providers.lastTenantID != "" {
		t.Errorf("provider store was called with tenantID=%q; empty tenant_id must never reach either store", providers.lastTenantID)
	}
	if len(policy.tenantIDs) != 0 {
		t.Errorf("policy store was called %d times; empty tenant_id must never reach either store", len(policy.tenantIDs))
	}
}

// TestDeriveAuthPosturePolicyReadErrorFailsOpenLocalLoginOffered proves the
// fail-open behavior this derivation intentionally mirrors from
// SignInPolicyReadHandler.handlePublicGet: a transient policy-store outage
// must never hide the local login form (this field is a UX hint, never the
// real enforcement boundary — requireSSODecision in
// local_identity_sign_in_policy_gate.go is unaffected by it).
func TestDeriveAuthPosturePolicyReadErrorFailsOpenLocalLoginOffered(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant_a": {{ProviderConfigID: "okta-oidc", DisplayLabel: "x", ProviderKind: "oidc"}},
		},
	}
	policy := &fakeSignInPolicyReadStore{err: errors.New("policy store unavailable")}

	posture, err := DeriveAuthPosture(context.Background(), providers, policy, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v, want nil (policy read failure fails open)", err)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true on policy-store error (fail open)")
	}
	if len(posture.Providers) != 1 {
		t.Errorf("Providers = %#v, want the provider list unaffected by the policy error", posture.Providers)
	}
}

// TestDeriveAuthPostureNilPolicyStoreDefaultsLocalLoginOffered proves a nil
// SignInPolicyReadStore (matching AuthProviderListHandler's existing nil-safe
// convention for a test/dev environment without the sign-in policy wired)
// defaults to local login offered rather than panicking.
func TestDeriveAuthPostureNilPolicyStoreDefaultsLocalLoginOffered(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{"tenant_a": {}}}

	posture, err := DeriveAuthPosture(context.Background(), providers, nil, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true when no policy store is wired")
	}
}

// TestDeriveAuthPostureProviderListErrorPropagates proves a provider-store
// failure is NOT swallowed (unlike the policy-read fail-open path): the
// provider list is the primary discovery signal, so a failure there must
// surface as a 500 from the handler rather than silently rendering an empty
// login picker, matching AuthProviderListHandler's pre-existing error
// handling for ListLoginProviders.
func TestDeriveAuthPostureProviderListErrorPropagates(t *testing.T) {
	t.Parallel()

	providers := &fakeAuthProviderStore{err: errors.New("provider store unavailable")}
	policy := &fakeSignInPolicyReadStore{policy: SignInPolicy{}}

	_, err := DeriveAuthPosture(context.Background(), providers, policy, "tenant_a")
	if err == nil {
		t.Fatal("DeriveAuthPosture() error = nil, want the provider-store error to propagate")
	}
}

// TestDeriveAuthPostureNilProviderStoreDefaultsSafe proves a nil
// AuthProviderStore (matching AuthProviderListHandler's h.Store nil-safe
// convention) returns the safe empty-posture default instead of panicking.
func TestDeriveAuthPostureNilProviderStoreDefaultsSafe(t *testing.T) {
	t.Parallel()

	posture, err := DeriveAuthPosture(context.Background(), nil, nil, "tenant_a")
	if err != nil {
		t.Fatalf("DeriveAuthPosture() error = %v", err)
	}
	if len(posture.Providers) != 0 {
		t.Errorf("Providers = %#v, want empty", posture.Providers)
	}
	if !posture.LocalLoginOffered {
		t.Error("LocalLoginOffered = false, want true (safe default)")
	}
}
