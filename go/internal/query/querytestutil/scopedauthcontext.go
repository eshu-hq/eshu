// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "github.com/eshu-hq/eshu/go/internal/query/queryauth"

// ScopedTestAuthContext builds the scoped auth context scope-enforcement tests
// exercise: tenant identity with an explicit repository allow-list.
//
// It lives here rather than in a package query test file because of the Go
// rule documented on FakeScopedTokenResolver (scopedtoken.go): the moved
// impact/ investigation tests build scoped contexts from outside package
// query, so the constructor must be importable. query.AuthContext is an alias
// of queryauth.AuthContext, so this builds exactly the value the root helper
// built.
func ScopedTestAuthContext(tenant string, allowedRepositoryIDs []string) queryauth.AuthContext {
	return queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             tenant,
		WorkspaceID:          tenant,
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:" + tenant,
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}
