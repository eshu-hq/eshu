// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// RotateLocalIdentityPassword self-service rotates a local credential's
// password: the caller must re-prove the CURRENT password (and MFA
// recovery-code proof, if the account has an active MFA factor) before the
// new password is accepted. This is issue #4976's forced-rotation surface —
// it is deliberately reachable without an existing session, because a
// must-change-password credential (the ESHU_ADMIN_USERNAME/PASSWORD[_FILE]
// -seeded bootstrap admin) can never obtain one any other way:
// AuthenticateLocalIdentity refuses to issue a session for such a credential
// (see the MustChangePassword check in identity_local.go).
//
// Rotation is not gated on MustChangePassword being true: any local user may
// self-service rotate their own password through this path, and doing so
// always clears MustChangePassword (a no-op when it was already false). This
// keeps the enforcement logic single-purpose (one check, one place) rather
// than special-casing "forced" vs "voluntary" rotation.
//
// The credential read-verify-write happens inside one transaction using
// selectLocalIdentityCredentialForUpdateQuery ("FOR UPDATE OF c"): a
// concurrent second rotation attempt against the same credential blocks on
// the row lock, then — under Read Committed's EvalPlanQual recheck —
// re-evaluates "c.status = 'active'" against the first rotation's committed
// result. Once the first rotation commits (revoking the old credential row),
// the second transaction's locked read returns no row, so a stale password
// can never be accepted twice.
func (s *IdentitySubjectStore) RotateLocalIdentityPassword(
	ctx context.Context,
	rotation LocalIdentityPasswordRotation,
) (LocalIdentityAuthenticationResult, error) {
	if s.db == nil {
		return LocalIdentityAuthenticationResult{}, errors.New("identity subject store database is required")
	}
	rotation = normalizePasswordRotation(rotation)
	if rotation.SubjectIDHash == "" || rotation.CurrentPassword == "" {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid}, nil
	}
	if err := validatePasswordRotation(rotation); err != nil {
		return LocalIdentityAuthenticationResult{}, err
	}
	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return LocalIdentityAuthenticationResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	row, ok, err := selectLocalIdentityCredentialForUpdate(ctx, tx, rotation.SubjectIDHash, rotation.Now)
	if err != nil {
		return LocalIdentityAuthenticationResult{}, err
	}
	if !ok {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthInvalid}, nil
	}
	if row.Status != "active" || !row.DisabledAt.IsZero() {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthDisabled}, nil
	}
	if !row.LockedUntil.IsZero() && row.LockedUntil.After(rotation.Now) {
		return LocalIdentityAuthenticationResult{Status: LocalIdentityAuthLocked, LockedUntil: row.LockedUntil}, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(rotation.CurrentPassword)) != nil {
		return s.recordFailedLocalIdentityAttempt(ctx, row, rotation.Now)
	}
	// MFA re-proof is required whenever the account has an active factor,
	// independent of admin role or the require_mfa_for_all_users policy: this
	// mirrors login's admin requirement (every admin has an MFA factor by
	// construction — see seed_initial_admin.go) and, for any non-admin that
	// happens to carry a factor, never skips a proof the account already
	// enrolled. It intentionally does not re-run login's
	// signInPolicyRequiresMFAForUsers policy read for a non-admin with NO
	// factor: rotation's purpose is proving possession of what the account
	// already has, not enrolling a new one.
	if row.HasActiveMFA {
		if rotation.MFARecoveryCodeHash == "" {
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
		if err := consumeLocalIdentityRecoveryCode(ctx, tx, row.UserID, LocalIdentityAuthenticationAttempt{
			MFARecoveryCodeHash:   rotation.MFARecoveryCodeHash,
			ConsumeRecoveryCodeAt: rotation.ConsumeRecoveryCodeAt,
			Now:                   rotation.Now,
		}); err != nil {
			if errors.Is(err, errLocalIdentityRecoveryCodeInvalid) {
				return s.recordFailedLocalIdentityAttempt(ctx, row, rotation.Now)
			}
			return LocalIdentityAuthenticationResult{}, err
		}
	}
	// Password (and MFA, when required) both proven. Revoke the old
	// credential and insert the replacement inside the same transaction as
	// the row lock above, so the rotation is atomic: a crash between these
	// two statements rolls back to the pre-rotation state rather than
	// stranding the credential with no active row.
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityCredentialsQuery, row.UserID, rotation.Now); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("revoke local identity credential for rotation: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		insertLocalIdentityCredentialQuery,
		rotation.CredentialID,
		row.UserID,
		rotation.NewPasswordHash,
		rotation.NewPasswordAlgorithm,
		rotation.NewPasswordParametersHash,
		rotation.Now,
		false, // rotation always clears must_change_password
	); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("insert rotated local identity credential: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("commit local identity password rotation: %w", err)
	}
	committed = true
	return s.finishLocalIdentityAuthentication(ctx, row, rotation.Now)
}

// finishLocalIdentityAuthentication is the shared tail for every path that
// has already proven a local credential (password, and MFA when required):
// production login (AuthenticateLocalIdentity, identity_local.go) and
// self-service password rotation (RotateLocalIdentityPassword above, issue
// #4976). It clears lockout state, destroys the bootstrap credential
// envelope on the subject's first successful proof, resolves the non-admin
// permission-catalog snapshot, and returns an authenticated result the
// caller can issue a session from. Keeping this in one place means the two
// callers can never drift on what "successfully authenticated" means.
func (s *IdentitySubjectStore) finishLocalIdentityAuthentication(
	ctx context.Context,
	row localIdentityCredentialRow,
	now time.Time,
) (LocalIdentityAuthenticationResult, error) {
	if _, err := s.db.ExecContext(ctx, clearLocalIdentityFailedAttemptsQuery, row.UserID); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("clear local identity failed attempts: %w", err)
	}
	// Destroy the one-time bootstrap credential envelope on this subject's
	// first successful login (epic #4962/#4963). This is a no-op for every
	// login except the bootstrap admin's very first one: no matching row
	// (env-seeded or sso-only/disabled bootstrap mode), or a row already
	// consumed by an earlier login, both leave affected=0. For the forced-
	// rotation flow this is also the first point the env-seeded admin's
	// bootstrap credential is destroyed: AuthenticateLocalIdentity's
	// MustChangePassword branch returns before reaching this tail, so the
	// envelope survives the blocked initial login attempt and is only
	// consumed once rotation actually succeeds.
	if _, err := s.ConsumeBootstrapCredential(ctx, row.TenantID, row.WorkspaceID, row.SubjectIDHash, now); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("consume bootstrap credential: %w", err)
	}
	auth := LocalIdentityAuthContext{
		TenantID:           row.TenantID,
		WorkspaceID:        row.WorkspaceID,
		SubjectIDHash:      row.SubjectIDHash,
		SubjectClass:       "local_user",
		PolicyRevisionHash: row.PolicyRevisionHash,
		AllScopes:          row.HasAdminRole,
	}
	// All-scope (admin/owner) sessions stay fail-open exactly as before: no
	// enforcement snapshot is attached. Only non-admin sessions carry the
	// permission-catalog grant snapshot so the catalog enforces them.
	if !auth.AllScopes {
		roles, err := s.resolveLocalIdentityRoles(ctx, row.TenantID, row.WorkspaceID, row.UserID, now)
		if err != nil {
			// Fails closed (no session issued). Log distinctly so an operator can
			// tell a permission-catalog resolution outage from any other login 500.
			slog.ErrorContext(ctx, "local session role resolution failed; login denied",
				"subject_class", "local_user", "tenant_id", row.TenantID, "error", err)
			return LocalIdentityAuthenticationResult{}, err
		}
		features, dataClasses, err := resolvePermissionGrantsForRoles(ctx, s.db, row.TenantID, roles, now)
		if err != nil {
			slog.ErrorContext(ctx, "local session permission grant resolution failed; login denied",
				"subject_class", "local_user", "tenant_id", row.TenantID, "role_count", len(roles), "error", err)
			return LocalIdentityAuthenticationResult{}, err
		}
		auth.RoleIDs = roles
		auth.PermissionCatalogEnforced = true
		auth.AllowedPermissionFeatures = features
		auth.AllowedPermissionDataClasses = dataClasses
	}
	return LocalIdentityAuthenticationResult{
		Status:        LocalIdentityAuthAuthenticated,
		Authenticated: true,
		Auth:          auth,
	}, nil
}
