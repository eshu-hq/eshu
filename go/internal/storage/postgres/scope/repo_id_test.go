// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopestore

import "testing"

func TestRepoIDFromScopeID(t *testing.T) {
	cases := []struct {
		name    string
		scopeID string
		want    string
	}{
		{"git repository scope lowercases repo id", "git-repository-scope:GitHub.com/Org/App", "github.com/org/app"},
		{"git repository scope trims whitespace", "git-repository-scope:  repo-a  ", "repo-a"},
		{"gcp relationship scope has no single own repo id", "gcp:project:hoist:relationship:global", ""},
		{"empty scope id", "", ""},
		{"unrelated scope prefix", "vault-cluster-scope:cluster-a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RepoIDFromScopeID(tc.scopeID)
			if got != tc.want {
				t.Fatalf("RepoIDFromScopeID(%q) = %q, want %q", tc.scopeID, got, tc.want)
			}
		})
	}
}
