// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"slices"
	"testing"
)

func TestCanonicalRepositoryIDForScopeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		scopeID string
		want    string
	}{
		{name: "git repository scope", scopeID: "git-repository-scope:repository:r_payments", want: "repository:r_payments"},
		{name: "surrounding space", scopeID: "  git-repository-scope:repository:r_payments  ", want: "repository:r_payments"},
		{name: "canonical id is not a scope id", scopeID: "repository:r_payments", want: ""},
		{name: "another collector's scope", scopeID: "aws-account-scope:1234567890", want: ""},
		{name: "empty remainder", scopeID: "git-repository-scope:", want: ""},
		// A ref scope names one ref of the repository. The rows a read returns
		// carry no ref, so resolving it would hand the caller every ref.
		{name: "repository ref scope", scopeID: "git-repository-scope:repository:r_payments@main", want: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := CanonicalRepositoryIDForScopeID(testCase.scopeID); got != testCase.want {
				t.Fatalf("CanonicalRepositoryIDForScopeID(%q) = %q, want %q", testCase.scopeID, got, testCase.want)
			}
		})
	}
}

func TestWithCanonicalScopeRepositoriesOnlyAdds(t *testing.T) {
	t.Parallel()

	t.Run("scope grant resolves to its repository", func(t *testing.T) {
		t.Parallel()

		widened := RepositoryAccessFilter{
			AllowedScopeIDs: []string{"git-repository-scope:repository:r_payments"},
			Allowed:         map[string]struct{}{"git-repository-scope:repository:r_payments": {}},
		}.WithCanonicalScopeRepositories()

		if !slices.Contains(widened.AllowedRepositoryIDs, "repository:r_payments") {
			t.Fatalf("AllowedRepositoryIDs = %#v, want the canonical id the granted scope names", widened.AllowedRepositoryIDs)
		}
		if !widened.AllowsRepositoryID("repository:r_payments") {
			t.Fatal("AllowsRepositoryID() = false for the repository the granted scope owns")
		}
		if !slices.Contains(widened.AllowedScopeIDs, "git-repository-scope:repository:r_payments") {
			t.Fatalf("AllowedScopeIDs = %#v, want the granted scope id kept", widened.AllowedScopeIDs)
		}
	})

	t.Run("a scope that names no repository grants nothing new", func(t *testing.T) {
		t.Parallel()

		widened := RepositoryAccessFilter{
			AllowedScopeIDs: []string{"aws-account-scope:1234567890"},
			Allowed:         map[string]struct{}{"aws-account-scope:1234567890": {}},
		}.WithCanonicalScopeRepositories()

		if got := widened.RepositorySearchIDs(); !slices.Equal(got, []string{"aws-account-scope:1234567890"}) {
			t.Fatalf("RepositorySearchIDs() = %#v, want the grant unchanged and never empty", got)
		}
	})

	t.Run("an unscoped caller is unchanged", func(t *testing.T) {
		t.Parallel()

		widened := RepositoryAccessFilter{AllScopes: true}.WithCanonicalScopeRepositories()
		if !widened.AllScopes || len(widened.AllowedRepositoryIDs) != 0 {
			t.Fatalf("WithCanonicalScopeRepositories() = %#v, want an unscoped filter unchanged", widened)
		}
	})

	t.Run("a grantless caller stays empty", func(t *testing.T) {
		t.Parallel()

		if widened := (RepositoryAccessFilter{}).WithCanonicalScopeRepositories(); !widened.Empty() {
			t.Fatalf("Empty() = false after widening a grantless filter: %#v", widened)
		}
	})

	t.Run("a repository already granted is not duplicated", func(t *testing.T) {
		t.Parallel()

		widened := RepositoryAccessFilter{
			AllowedScopeIDs:      []string{"git-repository-scope:repository:r_payments"},
			AllowedRepositoryIDs: []string{"repository:r_payments"},
		}.WithCanonicalScopeRepositories()

		if got := widened.AllowedRepositoryIDs; !slices.Equal(got, []string{"repository:r_payments"}) {
			t.Fatalf("AllowedRepositoryIDs = %#v, want no duplicate of an id already granted", got)
		}
	})
}
