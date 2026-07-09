// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// adminMutationFakeDB is a programmable ExecQueryer+Beginner that records the
// statements it executes and returns canned rows, so the store's validation
// and idempotency branching can be proven without a real Postgres.
//
// Concurrency note: this fake drives SQL-shape and idempotency assertions
// only. Real concurrent write correctness (ON CONFLICT PK convergence, 23503
// FK race) is bounded by the package's fake-DB convention; it is not
// exercised here.
type adminMutationFakeDB struct {
	roleActive     bool
	providerActive bool
	memberActive   bool // controls activeMembershipExists result

	// invitation state for the revoke read-then-write path.
	inviteFound    bool
	inviteStatus   string
	inviteRevoked  bool
	inviteAccepted bool
	inviteExpired  bool // simulates an active invitation whose expires_at is in the past

	upsertInserted bool
	upsertStatus   string
	upsertRef      string

	deleteMatches bool

	execQueries  []string
	queryQueries []string
	tenantArgs   []string
}

func (db *adminMutationFakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execQueries = append(db.execQueries, query)
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			db.tenantArgs = append(db.tenantArgs, s)
		}
	}
	// revoke role assignment affects one row only when an active row exists.
	if strings.Contains(query, "UPDATE identity_membership_roles") {
		if db.deleteMatches {
			return affectedResult{affected: 1}, nil
		}
		return affectedResult{affected: 0}, nil
	}
	// revoke invitation affects one row when active.
	if strings.Contains(query, "UPDATE identity_invitations") {
		return affectedResult{affected: 1}, nil
	}
	return affectedResult{affected: 0}, nil
}

func (db *adminMutationFakeDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.queryQueries = append(db.queryQueries, query)
	switch {
	case strings.Contains(query, "FROM identity_roles"):
		if db.roleActive {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case strings.Contains(query, "FROM identity_tenant_memberships"):
		if db.memberActive {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case strings.Contains(query, "FROM identity_provider_configs"):
		if db.providerActive {
			return &scalarRows{data: [][]any{{1}}}, nil
		}
		return &scalarRows{}, nil
	case strings.Contains(query, "FROM identity_invitations"):
		if !db.inviteFound {
			return &scalarRows{}, nil
		}
		var revoked, accepted, expires any
		if db.inviteRevoked {
			revoked = time.Now().UTC()
		}
		if db.inviteAccepted {
			accepted = time.Now().UTC()
		}
		if db.inviteExpired {
			// One hour in the past — clearly expired relative to any RevokedAt.
			expires = time.Now().UTC().Add(-time.Hour)
		}
		return &scalarRows{data: [][]any{{db.inviteStatus, revoked, accepted, expires}}}, nil
	case strings.Contains(query, "INSERT INTO identity_membership_roles"):
		return &scalarRows{data: [][]any{{db.upsertStatus, db.upsertInserted}}}, nil
	case strings.Contains(query, "INSERT INTO identity_provider_group_role_mappings"):
		return &scalarRows{data: [][]any{{db.upsertRef, db.upsertStatus, db.upsertInserted}}}, nil
	case strings.Contains(query, "UPDATE identity_provider_group_role_mappings"):
		if db.deleteMatches {
			return &scalarRows{data: [][]any{{"prov_1"}}}, nil
		}
		return &scalarRows{}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (db *adminMutationFakeDB) Begin(context.Context) (Transaction, error) {
	return &adminMutationFakeTx{db: db}, nil
}

// adminMutationFakeTx delegates to the parent fake so the invitation revoke
// read-then-write path runs against the same canned state.
type adminMutationFakeTx struct {
	db *adminMutationFakeDB
}

func (tx *adminMutationFakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *adminMutationFakeTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx *adminMutationFakeTx) Commit() error   { return nil }
func (tx *adminMutationFakeTx) Rollback() error { return nil }
