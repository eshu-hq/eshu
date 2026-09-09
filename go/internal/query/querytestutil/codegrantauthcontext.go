// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// CodeGrantScopedAuthContext builds the scoped-token AuthContext the #5167
// code-family batch-1 proof set shares: tenant-a, granted exactly the
// repository ids passed in (nil for the empty-grant fail-closed case).
//
// It lives here rather than in a package query test file for the same reason
// documented on ScopedTestAuthContext (scopedauthcontext.go): as internal/query
// splits into handler-family subpackages (#6060), each family's tests need the
// same helpers, and they cannot reach root's _test.go declarations at all.
// query.AuthContext is an alias of queryauth.AuthContext, so this builds
// exactly the value the root helper built.
func CodeGrantScopedAuthContext(allowedRepositoryIDs []string) queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}

// AssertBoundRepositoryGrantArray proves a grant predicate's placeholder is
// actually bound to the caller's id list, not left dangling: a predicate whose
// parameter never arrives fails at execution, and a predicate bound to the
// wrong list silently widens the scan. Shipped builders bind the list with
// pgarray.Array, so the assertion scans args for the *pgarray.StringArray
// carrying want rather than demanding an exact string element.
func AssertBoundRepositoryGrantArray(t *testing.T, args []any, want []string) {
	t.Helper()
	for _, arg := range args {
		bound, ok := arg.(*pgarray.StringArray)
		if !ok {
			continue
		}
		if got := []string(*bound); slices.Equal(got, want) {
			return
		}
		t.Fatalf("bound grant array = %#v, want %#v", []string(*bound), want)
	}
	t.Fatalf("args = %#v, want one bound *pgarray.StringArray carrying %#v", args, want)
}
