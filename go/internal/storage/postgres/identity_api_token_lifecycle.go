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
	result, err := s.db.ExecContext(
		ctx,
		revokeLocalIdentityAPITokenQuery,
		revoke.TokenID,
		revoke.TenantID,
		revoke.WorkspaceID,
		revoke.RevokedAt,
	)
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

	result, err := tx.ExecContext(
		ctx,
		rotateLocalIdentityAPITokenQuery,
		rotate.NewTokenID,
		rotate.NewTokenHash,
		rotate.OldTokenID,
		rotate.TenantID,
		rotate.WorkspaceID,
		rotate.RotatedAt,
		nullTime(rotate.NewTokenExpires),
	)
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
