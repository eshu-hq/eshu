// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// seededIdentityQueryer is a stub identity-token store that resolves ONE real
// personal identity-token row through the full production query pipeline
// (resolveIdentityAPITokenSubjectQuery -> resolveIdentityPersonalAPITokenRolesQuery
// -> permission/target queries), rather than always reporting not-found like
// stubExecQueryer. It exists so
// TestMCPAuthBootstrapIdentityOnlyServesHeaderlessOpen can exercise a
// deployment where the always-wired Postgres identity resolver WOULD
// successfully resolve a presented credential (a real seeded bootstrap
// identity, #4962/#4963) -- proving headerless reads stay open because of the
// authEnforcementConfigured predicate, not because the identity resolver
// happens to have no data. stubExecQueryer's permanently-empty store cannot
// distinguish those two cases: this type can.
//
// Query dispatch matches on stable, unique substrings of the SQL text in
// go/internal/storage/postgres/identity_api_tokens_sql.go and
// oidc_login_schema.go rather than importing the unexported query constants
// (this is a different package). Each substring is chosen to be present in
// exactly one of the pipeline's queries; see the switch below for the mapping.
type seededIdentityQueryer struct {
	tokenHash          string
	tenantID           string
	workspaceID        string
	subjectIDHash      string
	policyRevisionHash string
	roleID             string
}

// QueryContext implements pgstatus.ExecQueryer, dispatching to the seeded
// subject/role row for a matching token hash, and an empty result for every
// other query in the resolution pipeline (permissions, scope targets,
// repository targets) -- an empty permission/target set does not stop
// ResolveIdentityAPITokenHash from succeeding once roles is non-empty.
func (q seededIdentityQueryer) QueryContext(_ context.Context, sqlText string, args ...any) (pgstatus.Rows, error) {
	switch {
	case strings.Contains(sqlText, "JOIN tenants ten"):
		// resolveIdentityAPITokenSubjectQuery: token_hash, token_class,
		// tenant_id, workspace_id, user subject hash, service principal id,
		// policy_revision_hash.
		if hash, _ := args[0].(string); hash != q.tokenHash {
			return emptyRows{}, nil
		}
		return newStaticRows([]staticRow{{
			q.tokenHash, "personal", q.tenantID, q.workspaceID, q.subjectIDHash, "", q.policyRevisionHash,
		}}), nil
	case strings.Contains(sqlText, "identity_membership_roles"):
		// resolveIdentityPersonalAPITokenRolesQuery: role_id, policy_revision_hash.
		if hash, _ := args[0].(string); hash != q.tokenHash {
			return emptyRows{}, nil
		}
		return newStaticRows([]staticRow{{q.roleID, q.policyRevisionHash}}), nil
	default:
		// identity_service_principal_roles (unused: this token is personal),
		// identity_role_grants (permissions), identity_role_scope_targets, and
		// identity_role_repository_targets all tolerate an empty result --
		// ResolveIdentityAPITokenHash only requires a non-empty role set.
		return emptyRows{}, nil
	}
}

// ExecContext implements pgstatus.ExecQueryer for
// MarkIdentityAPITokenUsedQuery, the single write ResolveIdentityAPITokenHash
// triggers on a successful match.
func (q seededIdentityQueryer) ExecContext(_ context.Context, sqlText string, _ ...any) (sql.Result, error) {
	if strings.Contains(sqlText, "UPDATE identity_token_metadata") {
		return staticResult{rowsAffected: 1}, nil
	}
	return nil, fmt.Errorf("seededIdentityQueryer: unexpected exec: %s", sqlText)
}

// staticResult is a minimal sql.Result for seededIdentityQueryer.ExecContext.
type staticResult struct{ rowsAffected int64 }

func (staticResult) LastInsertId() (int64, error) { return 0, nil }
func (r staticResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// staticRows is a fixed, in-memory pgstatus.Rows cursor over pre-built rows of
// string columns, standing in for a real *sql.Rows result set.
type staticRows struct {
	rows []staticRow
	idx  int
}

type staticRow []any

func newStaticRows(rows []staticRow) *staticRows {
	return &staticRows{rows: rows, idx: -1}
}

func (r *staticRows) Next() bool {
	r.idx++
	return r.idx < len(r.rows)
}

func (r *staticRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.rows) {
		return errors.New("staticRows: Scan called out of range")
	}
	row := r.rows[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("staticRows: Scan dest count %d != row width %d", len(dest), len(row))
	}
	for i, d := range dest {
		sp, ok := d.(*string)
		if !ok {
			return fmt.Errorf("staticRows: unsupported Scan destination %T at column %d", d, i)
		}
		s, ok := row[i].(string)
		if !ok {
			return fmt.Errorf("staticRows: column %d value %v is not a string", i, row[i])
		}
		*sp = s
	}
	return nil
}

func (r *staticRows) Err() error   { return nil }
func (r *staticRows) Close() error { return nil }
