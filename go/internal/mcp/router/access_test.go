// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestAccessAdmitsMirrorsMiddlewareRefusals pins the per-call filter to the
// refusals the API auth middleware always makes, and to admitting what it may
// admit under deployment policy.
func TestAccessAdmitsMirrorsMiddlewareRefusals(t *testing.T) {
	t.Parallel()

	allowlisted := routeClass{resolved: true, allowlisted: true}
	sharedOnly := routeClass{resolved: true, sharedKeyOnly: true}
	offAllowlist := routeClass{resolved: true}
	pending := routeClass{resolved: true, pendingRowFiltering: true}
	unresolved := routeClass{}

	scoped := access{present: true, auth: query.AuthContext{Mode: query.AuthModeScoped, AllowedRepositoryIDs: []string{"r"}}}
	blankMode := access{present: true, auth: query.AuthContext{}}
	shared := access{present: true, auth: query.AuthContext{Mode: query.AuthModeShared}}
	none := access{}
	consoleAdmin := access{present: true, auth: query.AuthContext{
		Mode: query.AuthModeBrowserSession, AllScopes: true, TenantID: "t", WorkspaceID: "w",
	}}
	consoleRestricted := access{present: true, auth: query.AuthContext{
		Mode: query.AuthModeBrowserSession, TenantID: "t", WorkspaceID: "w", AllowedScopeIDs: []string{"s"},
	}}
	tenantlessAdmin := access{present: true, auth: query.AuthContext{Mode: query.AuthModeBrowserSession, AllScopes: true}}

	cases := []struct {
		name  string
		acc   access
		class routeClass
		want  bool
	}{
		{"no auth admits shared-only", none, sharedOnly, true},
		{"no auth admits unresolved", none, unresolved, true},
		{"shared admits shared-only", shared, sharedOnly, true},
		{"scoped admits allowlisted", scoped, allowlisted, true},
		{"scoped refuses shared-only", scoped, sharedOnly, false},
		{"scoped refuses off-allowlist", scoped, offAllowlist, false},
		{"scoped refuses pending", scoped, pending, false},
		{"scoped refuses unresolved", scoped, unresolved, false},
		{"blank mode acts scoped", blankMode, offAllowlist, false},
		{"console admin refuses shared-only", consoleAdmin, sharedOnly, false},
		{"console admin may reach off-allowlist", consoleAdmin, offAllowlist, true},
		{"console admin may reach pending", consoleAdmin, pending, true},
		{"restricted console refuses off-allowlist", consoleRestricted, offAllowlist, false},
		{"restricted console admits allowlisted", consoleRestricted, allowlisted, true},
		{"tenantless admin refuses off-allowlist", tenantlessAdmin, offAllowlist, false},
	}
	for _, tc := range cases {
		if got := tc.acc.admits(tc.class); got != tc.want {
			t.Errorf("%s: admits = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestClassifyRouteUsesQueryLedgers proves the classification reads the real
// query ledgers rather than a copy.
func TestClassifyRouteUsesQueryLedgers(t *testing.T) {
	t.Parallel()

	if c := classifyRoute("POST", "/api/v0/code/cypher"); !c.sharedKeyOnly || c.allowlisted {
		t.Fatalf("cypher route class = %+v, want shared-key-only and not allowlisted", c)
	}
	if c := classifyRoute("POST", "/api/v0/code/search"); !c.allowlisted || c.sharedKeyOnly {
		t.Fatalf("code search route class = %+v, want allowlisted", c)
	}
	if c := classifyRoute("GET", "/api/v0/definitely-not-a-route"); c.allowlisted || c.sharedKeyOnly {
		t.Fatalf("unknown route class = %+v, want neither", c)
	}
}
