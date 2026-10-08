// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestKnownScopesHostFilterLive proves the github_org host filter against
// real Postgres: with host github.com only github.com scopes of the org are
// known, whether their remote was HTTPS or SSH (both stored normalized), and
// gitlab.com, GitHub Enterprise, and remote-less scopes drop out. With no host
// (explicit selectors) the read keeps the slug-only org partition.
//
// Runs in the live-postgres-readiness runner; see TestObservationStoreLive.
func TestKnownScopesHostFilterLive(t *testing.T) {
	ctx, sqlDB, store := openLiveStore(t)

	seedScopeRemote(ctx, t, sqlDB, "scope:gh-https", "repository", "git", "acme/a", "https://github.com/acme/a")
	seedScopeRemote(ctx, t, sqlDB, "scope:gh-ssh", "repository", "git", "acme/b",
		repositoryidentity.NormalizeRemoteURL("git@github.com:Acme/b.git"))
	seedScopeRemote(ctx, t, sqlDB, "scope:gitlab", "repository", "git", "acme/x", "https://gitlab.com/acme/x")
	seedScopeRemote(ctx, t, sqlDB, "scope:ghe", "repository", "git", "acme/y", "https://ghe.corp/acme/y")
	seedScopeRemote(ctx, t, sqlDB, "scope:no-remote", "repository", "git", "acme/z", "")

	for _, tc := range []struct {
		host string
		want []string
	}{
		{host: "github.com", want: []string{"scope:gh-https", "scope:gh-ssh"}},
		{host: "", want: []string{"scope:gh-https", "scope:gh-ssh", "scope:ghe", "scope:gitlab", "scope:no-remote"}},
	} {
		known, err := store.KnownScopes(ctx, "acme", tc.host)
		if err != nil {
			t.Fatalf("KnownScopes(acme, %q) error = %v", tc.host, err)
		}
		var got []string
		for _, scope := range known {
			got = append(got, scope.ScopeID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("KnownScopes(acme, %q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}
