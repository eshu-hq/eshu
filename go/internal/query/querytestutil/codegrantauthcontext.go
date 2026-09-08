// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "github.com/eshu-hq/eshu/go/internal/query/queryauth"

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
