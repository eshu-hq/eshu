// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestAuthDiscoveryAndPolicyFailuresAnswerFixedText drives the provider
// discovery, OAuth protected-resource metadata, admin sign-in policy read and
// update, and profile routes through a failing store with a backend fault, a
// client cancel, and an inner cancel (#7674). The public sign-in-policy read
// is not here: it answers a store error with its fail-open 200, not a 5xx.
func TestAuthDiscoveryAndPolicyFailuresAnswerFixedText(t *testing.T) {
	routes := []failure.Route{
		{
			Name: "login provider discovery", Path: "/api/v0/auth/providers?tenant_id=tenant_a",
			Message: "failed to list login providers",
			Handler: func(err error) failure.Mounter {
				return &AuthProviderListHandler{Store: &fakeAuthProviderStore{err: err}}
			},
		},
		{
			Name: "oauth protected resource metadata", Path: "/.well-known/oauth-protected-resource",
			Message: "failed to derive auth posture",
			Handler: func(err error) failure.Mounter {
				return &OAuthProtectedResourceHandler{
					Providers: &fakeAuthProviderStore{err: err}, TenantID: "default",
					Resource: "https://eshu.example.test",
				}
			},
		},
		{
			Name: "admin sign-in policy read", Path: "/api/v0/auth/admin/sign-in-policy",
			Message: "failed to read sign-in policy", Scope: allScopeTenantAdminScope,
			Handler: func(err error) failure.Mounter {
				return &SignInPolicyReadHandler{Store: &fakeSignInPolicyReadStore{err: err}}
			},
		},
		{
			Name: "admin sign-in policy update", Method: http.MethodPatch, Path: "/api/v0/auth/admin/sign-in-policy",
			Body:    `{"allow_local_user_creation":true}`,
			Message: "failed to update sign-in policy", Scope: allScopeTenantAdminScope,
			Handler: func(err error) failure.Mounter {
				return &SignInPolicyMutationHandler{Store: &fakeSignInPolicyMutationStore{err: err}}
			},
		},
		{
			Name: "profile mfa status", Path: "/api/v0/auth/profile",
			Message: "failed to fetch mfa status", Scope: browserSessionScope,
			Handler: func(err error) failure.Mounter {
				return &ProfileHandler{LocalIdentityStore: &fakeLocalIdentityListStore{mfaErr: err}}
			},
		},
	}
	runAuthFailureSteps(t, failure.Suite{Routes: routes}, errorFlowKinds)
}

// allScopeTenantAdminScope is an all-scope browser-session admin bound to a
// tenant, the caller the admin sign-in policy routes accept.
func allScopeTenantAdminScope(ctx context.Context) context.Context {
	return ContextWithAuthContext(ctx, AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_a", SubjectIDHash: "sha256:admin",
	})
}
