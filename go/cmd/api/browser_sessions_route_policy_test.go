// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestBrowserSessionRoutePolicyAllowsOwnerConsoleOnlyOutsideHostedMultiTenant(t *testing.T) {
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
			policy := browserSessionRoutePolicy(query.GovernanceStatusConfig{Mode: tc.mode})
			if got := policy.AllowTenantBoundAllScopes; got != tc.want {
				t.Fatalf("AllowTenantBoundAllScopes = %t, want %t for mode %q", got, tc.want, tc.mode)
			}
		})
	}
}
