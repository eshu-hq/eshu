// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	localIdentityAPITokenClassPersonal         = "personal"
	localIdentityAPITokenClassServicePrincipal = "service_principal"
)

// CreateLocalIdentityAPIToken stores one generated API token hash for an active
// personal user or service-principal subject.
func (s *IdentitySubjectStore) CreateLocalIdentityAPIToken(
	ctx context.Context,
	token LocalIdentityAPITokenCreate,
) error {
	if s.db == nil {
		return fmt.Errorf("identity subject store database is required")
	}
	token = normalizeLocalIdentityAPITokenCreate(token)
	if err := validateLocalIdentityAPITokenCreate(token); err != nil {
		return err
	}
	query, subjectID := localIdentityAPITokenInsertQuery(token)
	result, err := s.db.ExecContext(
		ctx,
		query,
		token.TokenID,
		token.TokenHash,
		token.TenantID,
		token.WorkspaceID,
		subjectID,
		token.DisplayHandleHash,
		token.PolicyRevisionHash,
		token.IssuedAt,
		nullTime(token.ExpiresAt),
		token.DisplayLabel,
	)
	if err != nil {
		return fmt.Errorf("create local identity api token: %w", err)
	}
	return requireLocalIdentityAPITokenRowsAffected(result)
}

// RevokeLocalIdentityAPIToken revokes one active generated API token.
func (s *IdentitySubjectStore) RevokeLocalIdentityAPIToken(
	ctx context.Context,
	revoke LocalIdentityAPITokenRevoke,
) error {
	if s.db == nil {
		return fmt.Errorf("identity subject store database is required")
	}
	revoke = normalizeLocalIdentityAPITokenRevoke(revoke)
	if err := validateLocalIdentityAPITokenRevoke(revoke); err != nil {
		return err
	}
	query, args := revokeLocalIdentityAPITokenExec(revoke)
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("revoke local identity api token: %w", err)
	}
	return requireLocalIdentityAPITokenRowsAffected(result)
}

// RotateLocalIdentityAPIToken inserts a replacement token hash and revokes the
// old token in one transaction.
func (s *IdentitySubjectStore) RotateLocalIdentityAPIToken(
	ctx context.Context,
	rotate LocalIdentityAPITokenRotate,
) error {
	rotate = normalizeLocalIdentityAPITokenRotate(rotate)
	if err := validateLocalIdentityAPITokenRotate(rotate); err != nil {
		return err
	}
	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	insertQuery, insertArgs := rotateLocalIdentityAPITokenInsertExec(rotate)
	result, err := tx.ExecContext(ctx, insertQuery, insertArgs...)
	if err != nil {
		return fmt.Errorf("insert rotated local identity api token: %w", err)
	}
	if err := requireLocalIdentityAPITokenRowsAffected(result); err != nil {
		return err
	}
	result, err = tx.ExecContext(
		ctx,
		revokeLocalIdentityAPITokenQuery,
		rotate.OldTokenID,
		rotate.TenantID,
		rotate.WorkspaceID,
		rotate.RotatedAt,
	)
	if err != nil {
		return fmt.Errorf("revoke rotated local identity api token: %w", err)
	}
	if err := requireLocalIdentityAPITokenRowsAffected(result); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity api token rotation: %w", err)
	}
	committed = true
	return nil
}

func localIdentityAPITokenInsertQuery(token LocalIdentityAPITokenCreate) (string, string) {
	if token.TokenClass == localIdentityAPITokenClassServicePrincipal {
		return insertLocalIdentityServicePrincipalAPITokenQuery, token.ServicePrincipalID
	}
	return insertLocalIdentityPersonalAPITokenQuery, token.UserID
}

// revokeLocalIdentityAPITokenExec selects the revoke statement and its bound
// args. A blank OwnerSubjectIDHash keeps the unrestricted all-scope admin
// revoke; a non-blank value selects the ownership-scoped self-service revoke
// (issue #5164) and binds the caller's subject hash as $5.
func revokeLocalIdentityAPITokenExec(revoke LocalIdentityAPITokenRevoke) (string, []any) {
	args := []any{revoke.TokenID, revoke.TenantID, revoke.WorkspaceID, revoke.RevokedAt}
	if revoke.OwnerSubjectIDHash == "" {
		return revokeLocalIdentityAPITokenQuery, args
	}
	return revokeLocalIdentityAPITokenByOwnerQuery, append(args, revoke.OwnerSubjectIDHash)
}

// rotateLocalIdentityAPITokenInsertExec selects the replacement-insert statement
// and its bound args for a rotation. A blank OwnerSubjectIDHash keeps the
// unrestricted all-scope admin rotate; a non-blank value selects the
// ownership-scoped self-service rotate (issue #5164) and binds the caller's
// subject hash as $8, so the replacement is inserted only when the caller owns
// the old token. The follow-on revoke of the old token reuses the base
// unrestricted revoke statement: it can only fire after the ownership-gated
// insert affected a row, so the old token is already proven owned.
func rotateLocalIdentityAPITokenInsertExec(rotate LocalIdentityAPITokenRotate) (string, []any) {
	args := []any{
		rotate.NewTokenID,
		rotate.NewTokenHash,
		rotate.OldTokenID,
		rotate.TenantID,
		rotate.WorkspaceID,
		rotate.RotatedAt,
		nullTime(rotate.NewTokenExpires),
	}
	if rotate.OwnerSubjectIDHash == "" {
		return rotateLocalIdentityAPITokenQuery, args
	}
	return rotateLocalIdentityAPITokenByOwnerQuery, append(args, rotate.OwnerSubjectIDHash)
}

func requireLocalIdentityAPITokenRowsAffected(result sql.Result) error {
	if result == nil {
		return nil
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrLocalIdentityAPITokenUnavailable
	}
	return nil
}

func normalizeLocalIdentityAPITokenCreate(token LocalIdentityAPITokenCreate) LocalIdentityAPITokenCreate {
	token.TokenID = strings.TrimSpace(token.TokenID)
	token.TokenHash = strings.TrimSpace(token.TokenHash)
	token.TokenClass = strings.TrimSpace(token.TokenClass)
	token.TenantID = strings.TrimSpace(token.TenantID)
	token.WorkspaceID = strings.TrimSpace(token.WorkspaceID)
	token.UserID = strings.TrimSpace(token.UserID)
	token.ServicePrincipalID = strings.TrimSpace(token.ServicePrincipalID)
	token.DisplayHandleHash = strings.TrimSpace(token.DisplayHandleHash)
	token.DisplayLabel = strings.TrimSpace(token.DisplayLabel)
	token.PolicyRevisionHash = strings.TrimSpace(token.PolicyRevisionHash)
	token.IssuedAt = normalizeAPITokenTime(token.IssuedAt)
	token.ExpiresAt = normalizeAPITokenTime(token.ExpiresAt)
	if token.TokenClass == "" {
		token.TokenClass = localIdentityAPITokenClassPersonal
	}
	return token
}

func validateLocalIdentityAPITokenCreate(token LocalIdentityAPITokenCreate) error {
	if token.TokenID == "" || token.TokenHash == "" || token.TenantID == "" ||
		token.WorkspaceID == "" || token.PolicyRevisionHash == "" || token.IssuedAt.IsZero() {
		return errors.New("local identity api token create is incomplete")
	}
	switch token.TokenClass {
	case localIdentityAPITokenClassPersonal:
		if token.UserID == "" || token.ServicePrincipalID != "" {
			return errors.New("local identity personal api token requires user_id")
		}
	case localIdentityAPITokenClassServicePrincipal:
		if token.ServicePrincipalID == "" || token.UserID != "" {
			return errors.New("local identity service-principal api token requires service_principal_id")
		}
	default:
		return errors.New("local identity api token class is invalid")
	}
	if !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(token.IssuedAt) {
		return errors.New("local identity api token expires_at must be after issued_at")
	}
	return nil
}

func normalizeLocalIdentityAPITokenRevoke(revoke LocalIdentityAPITokenRevoke) LocalIdentityAPITokenRevoke {
	revoke.TokenID = strings.TrimSpace(revoke.TokenID)
	revoke.TenantID = strings.TrimSpace(revoke.TenantID)
	revoke.WorkspaceID = strings.TrimSpace(revoke.WorkspaceID)
	revoke.OwnerSubjectIDHash = strings.TrimSpace(revoke.OwnerSubjectIDHash)
	revoke.RevokedAt = normalizeAPITokenTime(revoke.RevokedAt)
	return revoke
}

func validateLocalIdentityAPITokenRevoke(revoke LocalIdentityAPITokenRevoke) error {
	if revoke.TokenID == "" || revoke.TenantID == "" || revoke.WorkspaceID == "" ||
		revoke.RevokedAt.IsZero() {
		return errors.New("local identity api token revoke is incomplete")
	}
	return nil
}

func normalizeLocalIdentityAPITokenRotate(rotate LocalIdentityAPITokenRotate) LocalIdentityAPITokenRotate {
	rotate.OldTokenID = strings.TrimSpace(rotate.OldTokenID)
	rotate.NewTokenID = strings.TrimSpace(rotate.NewTokenID)
	rotate.NewTokenHash = strings.TrimSpace(rotate.NewTokenHash)
	rotate.TenantID = strings.TrimSpace(rotate.TenantID)
	rotate.WorkspaceID = strings.TrimSpace(rotate.WorkspaceID)
	rotate.OwnerSubjectIDHash = strings.TrimSpace(rotate.OwnerSubjectIDHash)
	rotate.RotatedAt = normalizeAPITokenTime(rotate.RotatedAt)
	rotate.NewTokenExpires = normalizeAPITokenTime(rotate.NewTokenExpires)
	return rotate
}

func validateLocalIdentityAPITokenRotate(rotate LocalIdentityAPITokenRotate) error {
	if rotate.OldTokenID == "" || rotate.NewTokenID == "" || rotate.NewTokenHash == "" ||
		rotate.TenantID == "" || rotate.WorkspaceID == "" || rotate.RotatedAt.IsZero() {
		return errors.New("local identity api token rotation is incomplete")
	}
	if rotate.OldTokenID == rotate.NewTokenID {
		return errors.New("local identity api token rotation requires a new token id")
	}
	if !rotate.NewTokenExpires.IsZero() && !rotate.NewTokenExpires.After(rotate.RotatedAt) {
		return errors.New("local identity rotated api token expires_at must be after rotated_at")
	}
	return nil
}

func normalizeAPITokenTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}
