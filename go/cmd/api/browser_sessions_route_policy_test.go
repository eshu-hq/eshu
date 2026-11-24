// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestScopedRoutePolicyAllowsOwnerConsoleOnlyOutsideHostedMultiTenant pins the
// mode table cmd/api reads its all-scope opening from. The mapping itself
// moved into internal/query when cmd/mcp-server needed the same answer
// (#6450 residual item 1); this test stays in cmd/api because what it is
// really asserting is which posture THIS command ships with, and wrapAPIAuth
// is the caller that decides that.
func TestScopedRoutePolicyAllowsOwnerConsoleOnlyOutsideHostedMultiTenant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode string
		want bool
	}{
		{name: "unset local default", want: true},
		{name: "local no policy", mode: "local_no_policy", want: true},
		{name: "hosted single tenant", mode: "hosted_single_tenant", want: true},
		{name: "hosted multi tenant", mode: "hosted_multi_tenant", want: false},
		{name: "unknown mode", mode: "future_mode", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := query.ScopedRoutePolicyForGovernanceMode(query.GovernanceStatusConfig{Mode: tc.mode})
			if got := policy.AllowTenantBoundAllScopes; got != tc.want {
				t.Fatalf("AllowTenantBoundAllScopes = %t, want %t for mode %q", got, tc.want, tc.mode)
			}
		})
	}
}
