// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// probeIdentityToken is one identity-token row the capabilities-probe contract
// suite seeds. revoked and expiresAt model identity_token_metadata.revoked_at
// and expires_at. The stub applies the same predicate as the production SQL
// (resolveIdentityAPITokenSubjectQuery in
// go/internal/storage/postgres/identity_api_tokens_sql.go:
// `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $2)`) against the
// as-of time the resolver passes as $2, so a revoked or expired row is excluded
// only if the resolver supplies a correct now. The SQL text itself still needs a
// live Postgres to execute and is NOT proven here; the stub is a re-statement of
// that one predicate, not the database.
type probeIdentityToken struct {
	tokenHash          string
	tokenClass         string // "personal" or "service_principal"
	tenantID           string
	workspaceID        string
	subjectIDHash      string // personal tokens only
	servicePrincipalID string // service-principal tokens only
	policyRevisionHash string
	roleID             string
	features           []string   // granted permission features; nil means none
	revoked            bool       // revoked_at IS NOT NULL
	expiresAt          *time.Time // nil means non-expiring (expires_at IS NULL)
}

// activeAt mirrors `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > asOf)`.
func (t probeIdentityToken) activeAt(asOf time.Time) bool {
	return !t.revoked && (t.expiresAt == nil || t.expiresAt.After(asOf))
}

// probeIdentityQueryer serves the identity-token resolution pipeline for a set
// of seeded tokens. It extends the single-row seededIdentityQueryer pattern to
// both generated token classes and to per-token permission grants. Dispatch
// keys on stable substrings of the production SQL, as seededIdentityQueryer
// does.
type probeIdentityQueryer struct {
	tokens map[string]probeIdentityToken
	// err, when set, makes every query fail, modelling an unreachable store.
	err error
}

func newProbeIdentityQueryer(tokens ...probeIdentityToken) probeIdentityQueryer {
	q := probeIdentityQueryer{tokens: make(map[string]probeIdentityToken, len(tokens))}
	for _, token := range tokens {
		q.tokens[token.tokenHash] = token
	}
	return q
}

// QueryContext implements db.ExecQueryer.
func (q probeIdentityQueryer) QueryContext(_ context.Context, sqlText string, args ...any) (db.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	hash, _ := args[0].(string)
	asOf, _ := args[1].(time.Time)
	switch {
	case strings.Contains(sqlText, "JOIN tenants ten"):
		// token_hash, token_class, tenant_id, workspace_id, user subject hash,
		// service principal id, policy_revision_hash.
		token, ok := q.tokens[hash]
		if !ok || !token.activeAt(asOf) {
			return emptyRows{}, nil
		}
		return newStaticRows([]staticRow{{
			token.tokenHash, token.tokenClass, token.tenantID, token.workspaceID,
			token.subjectIDHash, token.servicePrincipalID, token.policyRevisionHash,
		}}), nil
	case strings.Contains(sqlText, "identity_membership_roles"),
		strings.Contains(sqlText, "identity_service_principal_roles"):
		// role_id, policy_revision_hash.
		token, ok := q.tokens[hash]
		if !ok || !token.activeAt(asOf) {
			return emptyRows{}, nil
		}
		return newStaticRows([]staticRow{{token.roleID, token.policyRevisionHash}}), nil
	case strings.Contains(sqlText, "identity_role_grants"):
		// feature, data_class. args are (tenant_id, role_ids, as_of, limit), so
		// this query is keyed by role, not by token hash.
		rows := []staticRow{}
		for _, token := range q.tokens {
			for _, feature := range token.features {
				if roleArg, _ := args[1].([]string); containsString(roleArg, token.roleID) {
					rows = append(rows, staticRow{feature, "public"})
				}
			}
		}
		return newStaticRows(rows), nil
	default:
		// Scope and repository targets: none granted.
		return emptyRows{}, nil
	}
}

// ExecContext implements db.ExecQueryer for the single last-used write a
// successful identity-token match triggers.
func (probeIdentityQueryer) ExecContext(_ context.Context, sqlText string, _ ...any) (sql.Result, error) {
	if strings.Contains(sqlText, "UPDATE identity_token_metadata") {
		return staticResult{rowsAffected: 1}, nil
	}
	return nil, fmt.Errorf("probeIdentityQueryer: unexpected exec: %s", sqlText)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var _ db.ExecQueryer = probeIdentityQueryer{}
