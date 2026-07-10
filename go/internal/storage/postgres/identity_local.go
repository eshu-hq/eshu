// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BootstrapLocalIdentity creates the first local owner/admin identity exactly
// once, guarded by a transaction-scoped advisory lock.
func (s *IdentitySubjectStore) BootstrapLocalIdentity(
	ctx context.Context,
	record LocalIdentityBootstrapRecord,
) error {
	record = normalizeBootstrapRecord(record)
	if err := validateBootstrapRecord(record); err != nil {
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

	if _, err := tx.ExecContext(ctx, localIdentityBootstrapLockQuery); err != nil {
		return fmt.Errorf("lock local identity bootstrap: %w", err)
	}
	count, err := countExistingLocalIdentityUsers(ctx, tx)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrLocalIdentityBootstrapCompleted
	}
	if err := insertBootstrapLocalIdentity(ctx, tx, record); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity bootstrap: %w", err)
	}
	committed = true
	return nil
}

// CreateLocalIdentityInvitation persists a hash-only invite for assignment-only
// local signup.
func (s *IdentitySubjectStore) CreateLocalIdentityInvitation(
	ctx context.Context,
	record LocalIdentityInvitationRecord,
) error {
	if s.db == nil {
		return errors.New("identity subject store database is required")
	}
	record = normalizeInvitationRecord(record)
	if err := validateInvitationRecord(record); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		createLocalIdentityInvitationQuery,
		record.InviteID,
		record.TenantID,
		record.WorkspaceID,
		record.InviteCodeHash,
		record.InviteeHandleHash,
		record.InviterSubjectIDHash,
		record.RoleID,
		record.Status,
		record.PolicyRevisionHash,
		record.ExpiresAt,
		record.CreatedAt,
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create local identity invitation: %w", err)
	}
	return nil
}

// AcceptLocalIdentityInvitation creates a user only from a live invitation.
func (s *IdentitySubjectStore) AcceptLocalIdentityInvitation(
	ctx context.Context,
	acceptance LocalIdentityInvitationAcceptance,
) error {
	acceptance = normalizeInvitationAcceptance(acceptance)
	if err := validateInvitationAcceptance(acceptance); err != nil {
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

	invite, ok, err := selectLocalIdentityInvitation(ctx, tx, acceptance.InviteCodeHash, acceptance.AcceptedAt)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLocalIdentityInvitationRequired
	}
	// Sign-in policy MFA-for-all-users gate (issue #4968): read inside this
	// same transaction, using the invite's own tenant_id, so the check and the
	// identity insert below observe a consistent snapshot.
	requireMFA, err := signInPolicyRequiresMFAForUsers(ctx, tx, invite.TenantID)
	if err != nil {
		return err
	}
	if requireMFA && acceptance.MFAFactorID == "" {
		return ErrLocalIdentityMFARequiredByPolicy
	}
	if err := insertInvitedLocalIdentity(ctx, tx, invite, acceptance); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		markLocalIdentityInvitationAcceptedQuery,
		invite.InviteID,
		acceptance.UserID,
		acceptance.AcceptedAt,
	); err != nil {
		return fmt.Errorf("mark local identity invitation accepted: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local identity invitation acceptance: %w", err)
	}
	committed = true
	return nil
}

// AuthenticateLocalIdentity validates a local password and required admin MFA.
func (s *IdentitySubjectStore) AuthenticateLocalIdentity(
	ctx context.Context,
	attempt LocalIdentityAuthenticationAttempt,
) (LocalIdentityAuthenticationResult, error) {
	if s.db == nil {
		return LocalIdentityAuthenticationResult{}, errors.New("identity subject store database is required")
	}
	attempt = normalizeAuthenticationAttempt(attempt)
	if attempt.SubjectIDHash == "" || attempt.Password == "" {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid}, nil
	}
	row, ok, err := selectLocalIdentityCredential(ctx, s.db, attempt.SubjectIDHash, attempt.Now)
	if err != nil {
		return LocalIdentityAuthenticationResult{}, err
	}
	if !ok {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid}, nil
	}
	if row.Status != "active" || !row.DisabledAt.IsZero() {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthDisabled}, nil
	}
	if !row.LockedUntil.IsZero() && row.LockedUntil.After(attempt.Now) {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthLocked, LockedUntil: row.LockedUntil}, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(attempt.Password)) != nil {
		return s.recordFailedLocalIdentityAttempt(ctx, row, attempt.Now)
	}
	// Forced password rotation (issue #4976): checked right after the password
	// proves out and BEFORE the MFA gate below. A must-change credential can
	// never obtain a session through this login path regardless of MFA, so
	// consuming the MFA recovery code here would burn it for nothing — and the
	// env-seeded admin is seeded with a SINGLE one-time recovery code, so a
	// consume here left it unable to complete RotateLocalIdentityPassword,
	// which demands that same code again (codex PR #5054 P1 review). Deferring
	// the MFA proof to rotation costs no security: the session is issued only
	// at rotation, after password AND MFA both prove there. No session and none
	// of the tail's session-issuance side effects run here; they happen inside
	// RotateLocalIdentityPassword (identity_local_rotate.go) once the caller
	// actually rotates.
	if row.MustChangePassword {
		return LocalIdentityAuthenticationResult{
			Status: LocalIdentityAuthMustChangePassword,
			Auth: LocalIdentityAuthContext{
				TenantID:           row.TenantID,
				WorkspaceID:        row.WorkspaceID,
				SubjectIDHash:      row.SubjectIDHash,
				SubjectClass:       "local_user",
				PolicyRevisionHash: row.PolicyRevisionHash,
				AllScopes:          row.HasAdminRole,
			},
		}, nil
	}
	// MFA at login is required unconditionally for every admin/owner, and for
	// every other local user once the tenant's require_mfa_for_all_users
	// sign-in policy is on (issue #5001, extending the invitation-accept-only
	// check from issue #4968). Before this change a non-admin was only ever
	// checked for MFA once, at invitation-accept time
	// (signInPolicyRequiresMFAForUsers in identity_sign_in_policy.go, enforced
	// in AcceptLocalIdentityInvitation above): a non-admin local user created
	// before the policy turned on, or created while it was off, was never
	// re-checked here at login. That gap only bit when require_sso=false
	// (require_sso=true already blocks non-admin local login entirely via
	// requireSSODecision in go/internal/query/local_identity_sign_in_policy_gate.go).
	//
	// Admins NEVER read this policy: an admin always requires MFA regardless
	// of its value, so reading it would only create a way for an
	// identity_sign_in_policies outage to deny a local ADMIN login before the
	// handler's documented policy_read_error_admin_allowed break-glass path
	// ever gets a chance to apply (issue #5001 P1 review finding). The read
	// happens only for a non-admin, and fails CLOSED there: a read error
	// denies that login rather than silently skipping the check, mirroring
	// the require_sso fail-closed stance for non-admins.
	mfaRequiredAtLogin := row.HasAdminRole
	if !row.HasAdminRole {
		requireMFAForAllUsers, err := signInPolicyRequiresMFAForUsers(ctx, s.db, row.TenantID)
		if err != nil {
			slog.ErrorContext(ctx, "local login mfa-for-all policy read failed; login denied",
				"subject_class", "local_user", "tenant_id", row.TenantID, "error", err)
			return LocalIdentityAuthenticationResult{}, err
		}
		mfaRequiredAtLogin = requireMFAForAllUsers
	}
	// Admin and non-admin share one enforcement block so the recovery-code
	// consumption and failed-attempt handling never drift between them.
	if mfaRequiredAtLogin {
		if !row.HasActiveMFA || attempt.MFARecoveryCodeHash == "" {
			// Auth carries only the fields the caller's require_sso gate needs
			// (go/internal/query handleLogin, issue #5001 P2 review finding):
			// a non-admin can never satisfy an MFA challenge through local
			// login when require_sso is ALSO on for the tenant, so the handler
			// must be able to run requireSSODecision against this identity
			// BEFORE surfacing mfa_required. The result stays unauthenticated
			// (Authenticated is not set) — no session is implied.
			return LocalIdentityAuthenticationResult{
				Status: LocalIdentityAuthMFARequired,
				Auth: LocalIdentityAuthContext{
					TenantID:           row.TenantID,
					WorkspaceID:        row.WorkspaceID,
					SubjectIDHash:      row.SubjectIDHash,
					SubjectClass:       "local_user",
					PolicyRevisionHash: row.PolicyRevisionHash,
					AllScopes:          row.HasAdminRole,
				},
			}, nil
		}
		if err := consumeLocalIdentityRecoveryCode(ctx, s.db, row.UserID, attempt); err != nil {
			if errors.Is(err, errLocalIdentityRecoveryCodeInvalid) {
				return s.recordFailedLocalIdentityAttempt(ctx, row, attempt.Now)
			}
			return LocalIdentityAuthenticationResult{}, err
		}
	}
	return s.finishLocalIdentityAuthentication(ctx, row, attempt.Now)
}

// resolveLocalIdentityRoles returns the active membership role IDs for one local
// user within a tenant/workspace as of the given time.
func (s *IdentitySubjectStore) resolveLocalIdentityRoles(
	ctx context.Context,
	tenantID string,
	workspaceID string,
	userID string,
	asOf time.Time,
) ([]string, error) {
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || workspaceID == "" || userID == "" {
		return nil, nil
	}
	if asOf.IsZero() {
		asOf = time.Now()
	}
	rows, err := s.db.QueryContext(
		ctx,
		resolveLocalIdentityRolesQuery,
		tenantID,
		workspaceID,
		userID,
		asOf.UTC(),
		maxOIDCGrantLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve local identity roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	roles := make([]string, 0)
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, fmt.Errorf("resolve local identity roles: %w", err)
		}
		roles = append(roles, roleID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve local identity roles: %w", err)
	}
	return cleanBrowserSessionStrings(roles), nil
}

// ResolveLocalIdentityBreakGlass returns an auth context only for a live,
// operator-enabled, unconsumed break-glass window.
func (s *IdentitySubjectStore) ResolveLocalIdentityBreakGlass(
	ctx context.Context,
	attempt LocalIdentityBreakGlassAttempt,
) (LocalIdentityAuthContext, error) {
	if s.db == nil {
		return LocalIdentityAuthContext{}, errors.New("identity subject store database is required")
	}
	attempt.BreakGlassCodeHash = strings.TrimSpace(attempt.BreakGlassCodeHash)
	if attempt.Now.IsZero() {
		attempt.Now = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(
		ctx,
		consumeLocalIdentityBreakGlassQuery,
		attempt.BreakGlassCodeHash,
		attempt.Now,
	)
	if err != nil {
		return LocalIdentityAuthContext{}, fmt.Errorf("resolve local identity break-glass: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return LocalIdentityAuthContext{}, ErrLocalIdentityBreakGlassUnavailable
	}
	var auth LocalIdentityAuthContext
	if err := rows.Scan(
		&auth.TenantID,
		&auth.WorkspaceID,
		&auth.SubjectIDHash,
		&auth.PolicyRevisionHash,
	); err != nil {
		return LocalIdentityAuthContext{}, fmt.Errorf("resolve local identity break-glass: %w", err)
	}
	auth.SubjectClass = "break_glass"
	auth.AllScopes = true
	return auth, rows.Err()
}

func (s *IdentitySubjectStore) beginLocalIdentityTx(ctx context.Context) (Transaction, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	beginner, ok := s.db.(Beginner)
	if !ok {
		return nil, ErrLocalIdentityTransactionRequired
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin local identity transaction: %w", err)
	}
	return tx, nil
}

func countExistingLocalIdentityUsers(ctx context.Context, db ExecQueryer) (int64, error) {
	rows, err := db.QueryContext(ctx, countExistingLocalIdentityUsersQuery)
	if err != nil {
		return 0, fmt.Errorf("count existing local identity users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.New("count existing local identity users returned no rows")
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("count existing local identity users: %w", err)
	}
	return count, rows.Err()
}

func insertBootstrapLocalIdentity(
	ctx context.Context,
	db ExecQueryer,
	record LocalIdentityBootstrapRecord,
) error {
	if _, err := db.ExecContext(ctx, upsertTenantRecordQuery, record.TenantID, "active", "", record.PolicyRevisionHash, record.CreatedAt, nullTime(time.Time{})); err != nil {
		return fmt.Errorf("upsert bootstrap tenant: %w", err)
	}
	if _, err := db.ExecContext(ctx, upsertWorkspaceRecordQuery, record.TenantID, record.WorkspaceID, "active", "", record.PolicyRevisionHash, record.CreatedAt, nullTime(time.Time{})); err != nil {
		return fmt.Errorf("upsert bootstrap workspace: %w", err)
	}
	if err := insertLocalIdentityUserCredential(ctx, db, localIdentityUserCredentialRecord{
		UserID:                 record.UserID,
		SubjectIDHash:          record.SubjectIDHash,
		ProfileHandleHash:      record.ProfileHandleHash,
		CredentialID:           localIdentityCredentialID(record.UserID, "initial"),
		PasswordHash:           record.PasswordHash,
		PasswordAlgorithm:      record.PasswordAlgorithm,
		PasswordParametersHash: record.PasswordParametersHash,
		CreatedAt:              record.CreatedAt,
		MustChangePassword:     record.MustChangePassword,
	}); err != nil {
		return err
	}
	if err := insertLocalIdentityMFA(ctx, db, record.UserID, record.MFAFactorID, record.MFAFactorKind, record.MFACredentialHandle, record.RecoveryCodeHashes, record.CreatedAt); err != nil {
		return err
	}
	return assignLocalIdentityRole(ctx, db, localIdentityRoleAssignment{
		TenantID:           record.TenantID,
		WorkspaceID:        record.WorkspaceID,
		UserID:             record.UserID,
		RoleID:             localIdentityOwnerRoleID,
		Source:             "bootstrap",
		PolicyRevisionHash: record.PolicyRevisionHash,
		AssignedAt:         record.CreatedAt,
	})
}

func insertInvitedLocalIdentity(
	ctx context.Context,
	db ExecQueryer,
	invite localIdentityInvitationRow,
	acceptance LocalIdentityInvitationAcceptance,
) error {
	if err := insertLocalIdentityUserCredential(ctx, db, localIdentityUserCredentialRecord{
		UserID:                 acceptance.UserID,
		SubjectIDHash:          acceptance.SubjectIDHash,
		ProfileHandleHash:      acceptance.ProfileHandleHash,
		CredentialID:           localIdentityCredentialID(acceptance.UserID, "initial"),
		PasswordHash:           acceptance.PasswordHash,
		PasswordAlgorithm:      acceptance.PasswordAlgorithm,
		PasswordParametersHash: acceptance.PasswordParametersHash,
		CreatedAt:              acceptance.AcceptedAt,
	}); err != nil {
		return err
	}
	if acceptance.MFAFactorID != "" {
		if err := insertLocalIdentityMFA(ctx, db, acceptance.UserID, acceptance.MFAFactorID, acceptance.MFAFactorKind, acceptance.MFACredentialHandle, acceptance.RecoveryCodeHashes, acceptance.AcceptedAt); err != nil {
			return err
		}
	}
	return assignLocalIdentityRole(ctx, db, localIdentityRoleAssignment{
		TenantID:           invite.TenantID,
		WorkspaceID:        invite.WorkspaceID,
		UserID:             acceptance.UserID,
		RoleID:             invite.RoleID,
		Source:             "invitation",
		PolicyRevisionHash: invite.PolicyRevisionHash,
		AssignedAt:         acceptance.AcceptedAt,
	})
}
