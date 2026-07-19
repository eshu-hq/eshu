// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests are the storage-layer security contract for issue #5164's
// self-service revoke/rotate: when the caller is a non-admin, the store must
// scope the mutation to a token the caller owns via an atomic ownership
// predicate bound to the caller's subject_id_hash, so a token the caller does
// not own affects zero rows and is reported as not-found. The all-scope admin
// path (empty OwnerSubjectIDHash) must keep using the unrestricted statement.

// TestRevokeLocalIdentityAPITokenByOwnerScopesToSubject proves the self-service
// revoke uses the ownership-scoped statement and binds the caller's subject
// hash, so the UPDATE cannot match a token owned by anyone else.
func TestRevokeLocalIdentityAPITokenByOwnerScopesToSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 1}}}
	store := NewIdentitySubjectStore(db)

	if err := store.RevokeLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRevoke{
		TokenID:            "tok_self",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		RevokedAt:          now,
		OwnerSubjectIDHash: "sha256:self",
	}); err != nil {
		t.Fatalf("RevokeLocalIdentityAPIToken() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1: %#v", len(db.execs), db.execs)
	}
	// The ownership predicate must resolve the caller's subject_id_hash to the
	// owning user_id (personal) or the service principal's owner_user_id (SP).
	for _, want := range []string{
		"UPDATE identity_token_metadata",
		"subject_id_hash = $5",
		"token_class = 'personal'",
		"token_class = 'service_principal'",
		"owner_user_id IN",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("owner-scoped revoke missing %q: %s", want, db.execs[0].query)
		}
	}
	// The caller's own subject hash must be bound as the ownership arg ($5).
	if got := db.execs[0].args; len(got) != 5 || got[4] != "sha256:self" {
		t.Fatalf("owner-scoped revoke args = %#v, want $5 = caller subject hash", got)
	}
}

// TestRevokeLocalIdentityAPITokenAdminOmitsOwnerPredicate proves the all-scope
// admin revoke (empty OwnerSubjectIDHash) keeps using the unrestricted
// statement with no ownership predicate and no fifth arg.
func TestRevokeLocalIdentityAPITokenAdminOmitsOwnerPredicate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 10, 5, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 1}}}
	store := NewIdentitySubjectStore(db)

	if err := store.RevokeLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRevoke{
		TokenID:     "tok_any",
		TenantID:    "tenant_local",
		WorkspaceID: "workspace_local",
		RevokedAt:   now,
	}); err != nil {
		t.Fatalf("RevokeLocalIdentityAPIToken() error = %v", err)
	}
	if fakeExecsContainQuery(db.execs, "subject_id_hash") {
		t.Fatalf("admin revoke leaked an ownership predicate: %s", db.execs[0].query)
	}
	if got := db.execs[0].args; len(got) != 4 {
		t.Fatalf("admin revoke args = %#v, want exactly 4 (no owner arg)", got)
	}
}

// TestRevokeLocalIdentityAPITokenByOwnerZeroRowsIsNotFound proves a self-service
// revoke that matches no owned active token reports the not-found sentinel the
// handler turns into a non-disclosing 404. This is the storage half of the
// cross-user denial: a non-owned token simply affects zero rows.
func TestRevokeLocalIdentityAPITokenByOwnerZeroRowsIsNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 10, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 0}}}
	store := NewIdentitySubjectStore(db)

	err := store.RevokeLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRevoke{
		TokenID:            "tok_victim",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		RevokedAt:          now,
		OwnerSubjectIDHash: "sha256:attacker",
	})
	if !errors.Is(err, ErrLocalIdentityAPITokenUnavailable) {
		t.Fatalf("RevokeLocalIdentityAPIToken() error = %v, want ErrLocalIdentityAPITokenUnavailable", err)
	}
}

// TestRotateLocalIdentityAPITokenByOwnerScopesToSubject proves the self-service
// rotate insert uses the ownership-scoped statement and binds the caller's
// subject hash as $8, so the replacement is created only for a token the caller
// owns.
func TestRotateLocalIdentityAPITokenByOwnerScopesToSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 10, 15, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	store := NewIdentitySubjectStore(db)

	if err := store.RotateLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRotate{
		OldTokenID:         "tok_self",
		NewTokenID:         "tok_new",
		NewTokenHash:       "sha256:new-generated-token",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		RotatedAt:          now,
		NewTokenExpires:    now.Add(7 * 24 * time.Hour),
		OwnerSubjectIDHash: "sha256:self",
	}); err != nil {
		t.Fatalf("RotateLocalIdentityAPIToken() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	// The insert (first exec) must carry the ownership predicate on old_token.
	insert := db.execs[0]
	for _, want := range []string{
		"INSERT INTO identity_token_metadata",
		"FROM identity_token_metadata old_token",
		"old_token.user_id IN",
		"subject_id_hash = $8",
	} {
		if !strings.Contains(insert.query, want) {
			t.Fatalf("owner-scoped rotate insert missing %q: %s", want, insert.query)
		}
	}
	if got := insert.args; len(got) != 8 || got[7] != "sha256:self" {
		t.Fatalf("owner-scoped rotate insert args = %#v, want $8 = caller subject hash", got)
	}
}

// TestRotateLocalIdentityAPITokenByOwnerZeroRowsIsNotFound proves a self-service
// rotate whose ownership-gated insert matches no owned active token reports the
// not-found sentinel and rolls the transaction back, so a caller cannot rotate
// a token they do not own.
func TestRotateLocalIdentityAPITokenByOwnerZeroRowsIsNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 10, 20, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{}
	db.execResults = []sql.Result{fakeResultWithRowsAffected{rowsAffected: 0}}
	store := NewIdentitySubjectStore(db)

	err := store.RotateLocalIdentityAPIToken(context.Background(), LocalIdentityAPITokenRotate{
		OldTokenID:         "tok_victim",
		NewTokenID:         "tok_new",
		NewTokenHash:       "sha256:new-generated-token",
		TenantID:           "tenant_local",
		WorkspaceID:        "workspace_local",
		RotatedAt:          now,
		OwnerSubjectIDHash: "sha256:attacker",
	})
	if !errors.Is(err, ErrLocalIdentityAPITokenUnavailable) {
		t.Fatalf("RotateLocalIdentityAPIToken() error = %v, want ErrLocalIdentityAPITokenUnavailable", err)
	}
	if db.committed {
		t.Fatalf("rotation committed despite a non-owned old token")
	}
}

// collapseWhitespace normalizes runs of whitespace to single spaces so query
// text can be substring-matched independent of SQL indentation.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestRevokeAndRotateByOwnerFilterInactiveOwners is the defense-in-depth
// regression guard requested during security review: the ownership predicate
// must resolve the caller's subject only through an ACTIVE, non-disabled,
// non-tombstoned identity — for both the personal owning user and the
// service-principal branch (the SP itself and its owning user). This keeps the
// by-owner path self-sufficient: a disabled or tombstoned owner's token cannot
// be revoked or rotated regardless of what the auth layer admits, because the
// ownership subquery returns no user_id and the mutation affects zero rows
// (which the handler renders as a non-disclosing 404). The filter columns and
// values mirror the sibling insertLocalIdentityPersonalAPITokenQuery and the
// bearer resolver (identity_api_tokens_sql.go).
func TestRevokeAndRotateByOwnerFilterInactiveOwners(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"revoke", revokeLocalIdentityAPITokenByOwnerQuery},
		{"rotate", rotateLocalIdentityAPITokenByOwnerQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			normalized := collapseWhitespace(tc.query)

			// The personal owning-user resolution must only match a live user.
			personalOwner := "FROM identity_users WHERE subject_id_hash = $" +
				ownerHashParam(tc.name) +
				" AND status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL"
			if !strings.Contains(normalized, personalOwner) {
				t.Fatalf("%s by-owner query does not filter inactive personal owners; want %q in:\n%s", tc.name, personalOwner, normalized)
			}

			// The service-principal branch must filter the SP's own liveness...
			spLiveness := "FROM identity_service_principals WHERE status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL"
			if !strings.Contains(normalized, spLiveness) {
				t.Fatalf("%s by-owner query does not filter inactive service principals; want %q in:\n%s", tc.name, spLiveness, normalized)
			}
			// ...and resolve the SP's owning user only when that user is live too.
			spOwner := "owner_user_id IN ( SELECT user_id FROM identity_users WHERE subject_id_hash = $" +
				ownerHashParam(tc.name) +
				" AND status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL )"
			if !strings.Contains(normalized, spOwner) {
				t.Fatalf("%s by-owner query does not filter inactive SP owners; want %q in:\n%s", tc.name, spOwner, normalized)
			}
		})
	}
}

// ownerHashParam returns the positional parameter index carrying the caller's
// subject_id_hash: $5 for the revoke UPDATE, $8 for the rotate INSERT-SELECT.
func ownerHashParam(queryName string) string {
	if queryName == "rotate" {
		return "8"
	}
	return "5"
}
