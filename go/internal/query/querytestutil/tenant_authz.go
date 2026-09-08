// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TenantAuthzRepositories is the shared three-repository fixture scoped-auth
// tests assert tenant isolation against: two same-named repositories in
// different tenant paths plus one shared repository. It moved here for #6060
// lane B B3 because the repository authz tests moved to the repository family
// package while code and entity authz tests stay in root; a _test.go
// declaration in either package is unreachable from the other.
func TenantAuthzRepositories() []querycontract.RepositoryCatalogEntry {
	return []querycontract.RepositoryCatalogEntry{
		{ID: "repo-team-a", Name: "payments", Path: "/team-a/payments"},
		{ID: "repo-team-b", Name: "payments", Path: "/team-b/payments"},
		{ID: "repo-shared", Name: "shared", Path: "/shared"},
	}
}
