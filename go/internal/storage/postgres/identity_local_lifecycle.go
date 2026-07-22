// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
)

// ResetLocalIdentityPassword revokes active credentials, writes the rotated
// bcrypt hash, and clears failed-attempt lockout state.
func (s *IdentitySubjectStore) ResetLocalIdentityPassword(
	ctx context.Context,
	reset LocalIdentityPasswordReset,
) error {
	reset = normalizePasswordReset(reset)
	if err := validatePasswordReset(reset); err != nil {
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

	if _, err := tx.ExecContext(ctx, revokeLocalIdentityCredentialsQuery, reset.UserID, reset.ResetAt); err != nil {
		return fmt.Errorf("revoke local identity credentials: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		insertLocalIdentityCredentialQuery,
		reset.CredentialID,
		reset.UserID,
		reset.PasswordHash,
		reset.PasswordAlgorithm,
		reset.PasswordParametersHash,
		reset.ResetAt,
		false, // an admin-driven reset always clears must_change_password too
	); err != nil {
		return fmt.Errorf("insert reset local identity credential: %w", err)
	}
	if _, err := tx.ExecContext(ctx, clearLocalIdentityFailedAttemptsQuery, reset.UserID); err != nil {
		return fmt.Errorf("clear local identity failed attempts after password reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity password reset: %w", err)
	}
	committed = true
	return nil
}

// ResetLocalIdentityMFA revokes active MFA factors and recovery codes before
// installing the replacement factor and recovery hashes. It first acquires
// lockLocalIdentityMFAReset's per-user advisory lock so two concurrent
// resets for the same user_id serialize instead of both landing an active
// factor row — see that function's doc comment for the race this closes and
// the lock-ordering invariant it establishes for future callers.
func (s *IdentitySubjectStore) ResetLocalIdentityMFA(
	ctx context.Context,
	reset LocalIdentityMFAReset,
) error {
	reset = normalizeMFAReset(reset)
	if err := validateMFAReset(reset); err != nil {
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

	if err := lockLocalIdentityMFAReset(ctx, tx, reset.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityRecoveryCodesQuery, reset.UserID, reset.ResetAt); err != nil {
		return fmt.Errorf("revoke local identity recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityMFAFactorsQuery, reset.UserID, reset.ResetAt); err != nil {
		return fmt.Errorf("revoke local identity mfa factors: %w", err)
	}
	if err := insertLocalIdentityMFA(
		ctx,
		tx,
		reset.UserID,
		reset.MFAFactorID,
		reset.MFAFactorKind,
		reset.MFACredentialHandle,
		reset.RecoveryCodeHashes,
		reset.ResetAt,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity mfa reset: %w", err)
	}
	committed = true
	return nil
}

// DisableLocalIdentityUser disables the user and revokes active local
// credentials, MFA factors, and browser sessions for the subject hash.
func (s *IdentitySubjectStore) DisableLocalIdentityUser(
	ctx context.Context,
	disable LocalIdentityDisableUser,
) error {
	disable = normalizeDisableUser(disable)
	if err := validateDisableUser(disable); err != nil {
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

	for _, exec := range []struct {
		query string
		name  string
	}{
		{disableLocalIdentityUserQuery, "disable local identity user"},
		{revokeLocalIdentityCredentialsQuery, "revoke local identity credentials"},
		{revokeLocalIdentityMFAFactorsQuery, "revoke local identity mfa factors"},
		{revokeLocalIdentityBrowserSessionsQuery, "revoke local identity browser sessions"},
	} {
		if _, err := tx.ExecContext(ctx, exec.query, disable.UserID, disable.DisabledAt); err != nil {
			return fmt.Errorf("%s: %w", exec.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity disable: %w", err)
	}
	committed = true
	return nil
}

// EnableLocalIdentityBreakGlass persists one active, time-boxed recovery
// window. Audit emission is handled by the query layer that authorizes it.
func (s *IdentitySubjectStore) EnableLocalIdentityBreakGlass(
	ctx context.Context,
	window LocalIdentityBreakGlassWindow,
) error {
	if s.db == nil {
		return fmt.Errorf("identity subject store database is required")
	}
	window = normalizeBreakGlassWindow(window)
	if err := validateBreakGlassWindow(window); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		enableLocalIdentityBreakGlassQuery,
		window.RecoveryID,
		window.TenantID,
		window.WorkspaceID,
		window.SubjectIDHash,
		window.BreakGlassCodeHash,
		window.Status,
		window.ReasonCode,
		window.PolicyRevisionHash,
		window.EnabledAt,
		window.ExpiresAt,
		window.CreatedAt,
		window.UpdatedAt,
	); err != nil {
		return fmt.Errorf("enable local identity break-glass: %w", err)
	}
	return nil
}
