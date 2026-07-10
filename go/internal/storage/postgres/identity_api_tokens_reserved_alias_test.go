// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// TestResolvePermissionGrantsQueryDoesNotAliasReservedWord is the hermetic,
// credential-free regression guard for the reserved-word alias bug: the
// resolveIdentityAPITokenPermissionsQuery once aliased identity_role_grants as
// `grant`, a Postgres reserved keyword, so every prepare/execute failed with
// `syntax error at or near "grant" (SQLSTATE 42601)`. That broke ALL
// permission-grant resolution — the DB-backed OIDC login callback (group-grant
// resolution) and scoped-token issuance both call resolvePermissionGrantsForRoles
// through this constant — so a DB-OIDC group-grant login could never complete.
//
// Because the failure is a Postgres parse error, only a live PREPARE proves it
// for real (TestResolvePermissionGrantsQueryPreparesAgainstRealPostgres below),
// and that proof SKIPS without ESHU_POSTGRES_DSN. This static guard is the CI
// mirror so the regression cannot silently reappear in a DSN-less lane: it
// asserts the shipped constant never aliases identity_role_grants as a reserved
// keyword. It goes RED against the pre-fix `identity_role_grants grant`.
func TestResolvePermissionGrantsQueryDoesNotAliasReservedWord(t *testing.T) {
	query := resolveIdentityAPITokenPermissionsQuery

	// Postgres reserved keywords that cannot be used as an unquoted alias.
	// `grant` is the one this bug shipped; the others guard equivalent slips.
	reserved := []string{"grant", "user", "table", "select", "order", "group"}
	for _, word := range reserved {
		if strings.Contains(query, "identity_role_grants "+word) {
			t.Fatalf(
				"resolveIdentityAPITokenPermissionsQuery aliases identity_role_grants as reserved keyword %q; "+
					"Postgres rejects it with SQLSTATE 42601. Use a non-reserved alias (e.g. role_grant).",
				word,
			)
		}
	}

	// A careless "fix" that drops the identity_roles join would also make the
	// reserved-word alias disappear while changing the authorization result.
	// Pin the join so the guard cannot be satisfied by deleting it.
	if !strings.Contains(query, "JOIN identity_roles role") {
		t.Fatalf("resolveIdentityAPITokenPermissionsQuery must retain the identity_roles join for correct grant scoping")
	}
}

// TestResolvePermissionGrantsQueryPreparesAgainstRealPostgres is the
// backend-required proof that the query parses against a real Postgres backend.
// Before the alias fix this PREPARE failed with SQLSTATE 42601; after it, the
// statement prepares cleanly. Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run 'ResolvePermissionGrantsQueryPrepares' -count=1
func TestResolvePermissionGrantsQueryPreparesAgainstRealPostgres(t *testing.T) {
	dsn := providerConfigLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres permission-grant query parse proof")
	}

	ctx := context.Background()
	db, _ := openProviderConfigLiveSchema(t, ctx, dsn)

	stmt, err := db.PrepareContext(ctx, resolveIdentityAPITokenPermissionsQuery)
	if err != nil {
		t.Fatalf("prepare resolveIdentityAPITokenPermissionsQuery against real Postgres: %v", err)
	}
	_ = stmt.Close()
}
