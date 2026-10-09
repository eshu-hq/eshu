// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "testing"

// TestIsGitDefaultBranchScopeEdgeCases pins the one predicate every recovery
// and admin caller uses to decide whether a scope id names a git
// default-branch repository scope, the only kind a per-repository reindex
// watermark can force (#7797). The semantics match
// querycontract.CanonicalRepositoryIDForScopeID: trim, cut the prefix, require
// a non-empty remainder, reject a ref scope.
func TestIsGitDefaultBranchScopeEdgeCases(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		scopeID string
		want    bool
	}{
		{"default branch scope", "git-repository-scope:repository:r_a", true},
		{"leading and trailing whitespace", "  git-repository-scope:repository:r_a\t", true},
		{"whitespace inside the remainder edges", "git-repository-scope:  repository:r_a  ", true},
		{"bare prefix", "git-repository-scope:", false},
		{"prefix with only whitespace after it", "git-repository-scope:   ", false},
		{"ref scope", "git-repository-scope:repository:r_a@release", false},
		{"ref scope with whitespace", " git-repository-scope:repository:r_a@release ", false},
		{"empty", "", false},
		{"only whitespace", "   ", false},
		{"non-git scope", "other-collector-scope:repository:r_a", false},
		{"prefix with different case", "Git-Repository-Scope:repository:r_a", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsGitDefaultBranchScope(tc.scopeID); got != tc.want {
				t.Fatalf("IsGitDefaultBranchScope(%q) = %v, want %v", tc.scopeID, got, tc.want)
			}
		})
	}
}
