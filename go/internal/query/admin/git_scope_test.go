// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// gitScopeEdgeCases is the edge set the shared git default-branch scope
// predicate and the query authorization decoder must classify the same way.
var gitScopeEdgeCases = []string{
	"git-repository-scope:repository:r_a",
	"  git-repository-scope:repository:r_a\t",
	"git-repository-scope:  repository:r_a  ",
	"git-repository-scope:",
	"git-repository-scope:   ",
	"git-repository-scope:repository:r_a@release",
	" git-repository-scope:repository:r_a@release ",
	"",
	"   ",
	"other-collector-scope:repository:r_a",
	"Git-Repository-Scope:repository:r_a",
}

// TestGitDefaultBranchScopeAgreesWithCanonicalRepositoryID keeps
// recovery.IsGitDefaultBranchScope and
// querycontract.CanonicalRepositoryIDForScopeID in agreement on every edge
// case. The recovery package must not import querycontract, so this package,
// which imports both, owns the check (#7797).
func TestGitDefaultBranchScopeAgreesWithCanonicalRepositoryID(t *testing.T) {
	t.Parallel()

	for _, scopeID := range gitScopeEdgeCases {
		shared := recovery.IsGitDefaultBranchScope(scopeID)
		decoded := querycontract.CanonicalRepositoryIDForScopeID(scopeID) != ""
		if shared != decoded {
			t.Fatalf("scope %q: recovery.IsGitDefaultBranchScope = %v, querycontract.CanonicalRepositoryIDForScopeID non-empty = %v",
				scopeID, shared, decoded)
		}
	}
}

// TestGitDefaultScopeMatchUsesTheSharedPredicate proves the admin reindex
// route classifies a catalog match with the shared predicate: a bare prefix
// and a ref scope are refused, a default-branch scope is accepted.
func TestGitDefaultScopeMatchUsesTheSharedPredicate(t *testing.T) {
	t.Parallel()

	for _, scopeID := range gitScopeEdgeCases {
		entry := catalogEntry("repository:r_a", scopeID)
		_, problem := gitDefaultScopeMatch([]querycontract.RepositoryCatalogEntry{entry})
		if accepted, want := problem == "", recovery.IsGitDefaultBranchScope(scopeID); accepted != want {
			t.Fatalf("gitDefaultScopeMatch(scope %q) accepted = %v (problem %q), want %v", scopeID, accepted, problem, want)
		}
	}
}
