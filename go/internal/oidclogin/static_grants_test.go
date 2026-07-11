// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"testing"
)

// TestStaticGrantResolverNeverPopulatesPolicyRevisionHash is the regression
// test for issue #5038: an operator-supplied role_grants[].policy_revision_hash
// in the static/env OIDC config must never be trusted. The DB-backed group
// mapping path always stamps policy_revision_hash server-side at INSERT time
// (browser_sessions_schema.go's createBrowserSessionQuery COALESCE); the
// static resolver must match that contract by leaving GrantResolution.
// PolicyRevisionHash empty, so the session-create COALESCE defaults it to the
// live workspace hash instead of trusting a hand-set value that can silently
// drift and 401 every subsequent authenticated request.
func TestStaticGrantResolverNeverPopulatesPolicyRevisionHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		grants []RoleGrant
	}{
		{
			name: "wrong non-empty operator-supplied hash is ignored",
			grants: []RoleGrant{{
				RoleID:             "developer",
				PolicyRevisionHash: "sha256:operator-supplied-wrong-hash",
			}},
		},
		{
			name: "empty operator-supplied hash is also fine",
			grants: []RoleGrant{{
				RoleID:             "developer",
				PolicyRevisionHash: "",
			}},
		},
		{
			name: "multiple roles with differing hashes no longer conflict",
			grants: []RoleGrant{
				{RoleID: "developer", PolicyRevisionHash: "sha256:one"},
				{RoleID: "viewer", PolicyRevisionHash: "sha256:two"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			roleIDs := make([]string, 0, len(tt.grants))
			for _, grant := range tt.grants {
				roleIDs = append(roleIDs, grant.RoleID)
			}
			resolver := StaticGrantResolver{
				GroupRoleMappings: []GroupRoleMapping{{
					Group:   "Eshu Developers",
					RoleIDs: roleIDs,
				}},
				RoleGrants: tt.grants,
			}

			resolution, ok, err := resolver.ResolveGroupGrants(context.Background(), GrantQuery{
				GroupHashes: []string{SHA256Hash("Eshu Developers")},
			})
			if err != nil {
				t.Fatalf("ResolveGroupGrants() error = %v", err)
			}
			if !ok {
				t.Fatal("ResolveGroupGrants() ok = false, want true")
			}
			if resolution.PolicyRevisionHash != "" {
				t.Fatalf("PolicyRevisionHash = %q, want empty so the session-create COALESCE resolves the live workspace hash", resolution.PolicyRevisionHash)
			}
		})
	}
}

// TestStaticGrantResolverRoleGrantIndexAllowsEmptyPolicyRevisionHash proves a
// role grant with no policy_revision_hash at all (the recommended shape going
// forward) still loads: the field is no longer a required part of the static
// config contract now that Eshu never reads it.
func TestStaticGrantResolverRoleGrantIndexAllowsEmptyPolicyRevisionHash(t *testing.T) {
	t.Parallel()

	resolver := StaticGrantResolver{
		RoleGrants: []RoleGrant{{RoleID: "developer"}},
	}
	if _, err := resolver.roleGrantIndex(); err != nil {
		t.Fatalf("roleGrantIndex() error = %v, want nil for a role grant with no policy_revision_hash", err)
	}
}

// TestStaticGrantResolverHasIgnoredPolicyRevisionHash covers the detector
// startup WARN logging depends on: it must report true only when at least one
// role grant sets the now-ignored field, so operators with stale configs get
// exactly one warning instead of silence or a hard failure.
func TestStaticGrantResolverHasIgnoredPolicyRevisionHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		grants []RoleGrant
		want   bool
	}{
		{name: "no role grants", grants: nil, want: false},
		{name: "role grants without the field", grants: []RoleGrant{{RoleID: "developer"}}, want: false},
		{
			name:   "one role grant sets the field",
			grants: []RoleGrant{{RoleID: "developer"}, {RoleID: "viewer", PolicyRevisionHash: "sha256:ignored"}},
			want:   true,
		},
		{
			name:   "whitespace-only value does not count",
			grants: []RoleGrant{{RoleID: "developer", PolicyRevisionHash: "   "}},
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolver := StaticGrantResolver{RoleGrants: tt.grants}
			if got := resolver.HasIgnoredPolicyRevisionHash(); got != tt.want {
				t.Fatalf("HasIgnoredPolicyRevisionHash() = %v, want %v", got, tt.want)
			}
		})
	}
}
