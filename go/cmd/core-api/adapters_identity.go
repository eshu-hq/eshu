// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// corePostgresLocalIdentityAdapter is the query-layer port for hash-only
// local identity persistence, backed by the Postgres identity subject
// store. Method-for-method it mirrors the retired cmd/api adapter:
// hash material crosses, plaintext never does.
type corePostgresLocalIdentityAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

var _ query.LocalIdentityStore = (*corePostgresLocalIdentityAdapter)(nil)

// newCorePostgresLocalIdentityAdapter builds the adapter, or nil without a
// database (every local-identity route then fails closed 503).
func newCorePostgresLocalIdentityAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *corePostgresLocalIdentityAdapter {
	if rawDB == nil {
		return nil
	}
	return &corePostgresLocalIdentityAdapter{
		store: pgstatus.NewIdentitySubjectStore(coreIdentityDB(rawDB, instruments)),
	}
}

// coreIdentityDB wraps a writer handle with identity-store telemetry.
func coreIdentityDB(rawDB *sql.DB, instruments *telemetry.Instruments) db.ExecQueryer {
	return coreInstrumentedDB(rawDB, instruments, "identity_subjects")
}

// BootstrapLocalIdentity persists a first-owner identity record.
func (a *corePostgresLocalIdentityAdapter) BootstrapLocalIdentity(
	ctx context.Context,
	record query.LocalIdentityBootstrapRecord,
) error {
	return a.store.BootstrapLocalIdentity(ctx, pgstatus.LocalIdentityBootstrapRecord{
		TenantID:               record.TenantID,
		WorkspaceID:            record.WorkspaceID,
		UserID:                 record.UserID,
		SubjectIDHash:          record.SubjectIDHash,
		ProfileHandleHash:      record.ProfileHandleHash,
		PasswordHash:           record.PasswordHash,
		PasswordAlgorithm:      record.PasswordAlgorithm,
		PasswordParametersHash: record.PasswordParametersHash,
		MFAFactorID:            record.MFAFactorID,
		MFAFactorKind:          record.MFAFactorKind,
		MFACredentialHandle:    record.MFACredentialHandle,
		RecoveryCodeHashes:     append([]string(nil), record.RecoveryCodeHashes...),
		PolicyRevisionHash:     record.PolicyRevisionHash,
		CreatedAt:              record.CreatedAt,
	})
}

// AuthenticateLocalIdentity validates a local credential proof.
func (a *corePostgresLocalIdentityAdapter) AuthenticateLocalIdentity(
	ctx context.Context,
	attempt query.LocalIdentityAuthenticationAttempt,
) (query.LocalIdentityAuthenticationResult, error) {
	result, err := a.store.AuthenticateLocalIdentity(ctx, pgstatus.LocalIdentityAuthenticationAttempt{
		SubjectIDHash:         attempt.SubjectIDHash,
		Password:              attempt.Password,
		MFARecoveryCodeHash:   attempt.MFARecoveryCodeHash,
		ConsumeRecoveryCodeAt: attempt.ConsumeRecoveryCodeAt,
		MFATOTPCode:           attempt.MFATOTPCode,
		Now:                   attempt.Now,
	})
	if err != nil {
		return query.LocalIdentityAuthenticationResult{}, err
	}
	return query.LocalIdentityAuthenticationResult{
		Status:        query.LocalIdentityAuthStatus(result.Status),
		Authenticated: result.Authenticated,
		Auth: query.LocalIdentityAuthContext{
			TenantID:                     result.Auth.TenantID,
			WorkspaceID:                  result.Auth.WorkspaceID,
			SubjectIDHash:                result.Auth.SubjectIDHash,
			SubjectClass:                 result.Auth.SubjectClass,
			PolicyRevisionHash:           result.Auth.PolicyRevisionHash,
			AllScopes:                    result.Auth.AllScopes,
			RoleIDs:                      append([]string(nil), result.Auth.RoleIDs...),
			PermissionCatalogEnforced:    result.Auth.PermissionCatalogEnforced,
			AllowedPermissionFeatures:    append([]string(nil), result.Auth.AllowedPermissionFeatures...),
			AllowedPermissionDataClasses: append([]string(nil), result.Auth.AllowedPermissionDataClasses...),
		},
		LockedUntil: result.LockedUntil,
	}, nil
}

// RotateLocalIdentityPassword rotates one user's password hash.
func (a *corePostgresLocalIdentityAdapter) RotateLocalIdentityPassword(
	ctx context.Context,
	rotation query.LocalIdentityPasswordRotation,
) (query.LocalIdentityAuthenticationResult, error) {
	result, err := a.store.RotateLocalIdentityPassword(ctx, pgstatus.LocalIdentityPasswordRotation{
		SubjectIDHash:             rotation.SubjectIDHash,
		CurrentPassword:           rotation.CurrentPassword,
		NewPasswordHash:           rotation.NewPasswordHash,
		NewPasswordAlgorithm:      rotation.NewPasswordAlgorithm,
		NewPasswordParametersHash: rotation.NewPasswordParametersHash,
		CredentialID:              rotation.CredentialID,
		MFARecoveryCodeHash:       rotation.MFARecoveryCodeHash,
		ConsumeRecoveryCodeAt:     rotation.ConsumeRecoveryCodeAt,
		MFATOTPCode:               rotation.MFATOTPCode,
		Now:                       rotation.Now,
	})
	if err != nil {
		return query.LocalIdentityAuthenticationResult{}, err
	}
	return query.LocalIdentityAuthenticationResult{
		Status:        query.LocalIdentityAuthStatus(result.Status),
		Authenticated: result.Authenticated,
		Auth: query.LocalIdentityAuthContext{
			TenantID:                     result.Auth.TenantID,
			WorkspaceID:                  result.Auth.WorkspaceID,
			SubjectIDHash:                result.Auth.SubjectIDHash,
			SubjectClass:                 result.Auth.SubjectClass,
			PolicyRevisionHash:           result.Auth.PolicyRevisionHash,
			AllScopes:                    result.Auth.AllScopes,
			RoleIDs:                      append([]string(nil), result.Auth.RoleIDs...),
			PermissionCatalogEnforced:    result.Auth.PermissionCatalogEnforced,
			AllowedPermissionFeatures:    append([]string(nil), result.Auth.AllowedPermissionFeatures...),
			AllowedPermissionDataClasses: append([]string(nil), result.Auth.AllowedPermissionDataClasses...),
		},
		LockedUntil: result.LockedUntil,
	}, nil
}

// CreateLocalIdentityInvitation persists a signup invitation.
func (a *corePostgresLocalIdentityAdapter) CreateLocalIdentityInvitation(
	ctx context.Context,
	record query.LocalIdentityInvitationRecord,
) error {
	return a.store.CreateLocalIdentityInvitation(ctx, pgstatus.LocalIdentityInvitationRecord{
		InviteID:             record.InviteID,
		TenantID:             record.TenantID,
		WorkspaceID:          record.WorkspaceID,
		InviteCodeHash:       record.InviteCodeHash,
		InviteeHandleHash:    record.InviteeHandleHash,
		InviterSubjectIDHash: record.InviterSubjectIDHash,
		RoleID:               record.RoleID,
		Status:               record.Status,
		PolicyRevisionHash:   record.PolicyRevisionHash,
		ExpiresAt:            record.ExpiresAt,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	})
}

// AcceptLocalIdentityInvitation creates a user from a live invitation.
func (a *corePostgresLocalIdentityAdapter) AcceptLocalIdentityInvitation(
	ctx context.Context,
	acceptance query.LocalIdentityInvitationAcceptance,
) error {
	return a.store.AcceptLocalIdentityInvitation(ctx, pgstatus.LocalIdentityInvitationAcceptance{
		InviteCodeHash:         acceptance.InviteCodeHash,
		UserID:                 acceptance.UserID,
		SubjectIDHash:          acceptance.SubjectIDHash,
		ProfileHandleHash:      acceptance.ProfileHandleHash,
		PasswordHash:           acceptance.PasswordHash,
		PasswordAlgorithm:      acceptance.PasswordAlgorithm,
		PasswordParametersHash: acceptance.PasswordParametersHash,
		MFAFactorID:            acceptance.MFAFactorID,
		MFAFactorKind:          acceptance.MFAFactorKind,
		MFACredentialHandle:    acceptance.MFACredentialHandle,
		RecoveryCodeHashes:     append([]string(nil), acceptance.RecoveryCodeHashes...),
		AcceptedAt:             acceptance.AcceptedAt,
	})
}

// ResetLocalIdentityPassword rotates a user's password hash by admin reset.
func (a *corePostgresLocalIdentityAdapter) ResetLocalIdentityPassword(
	ctx context.Context,
	reset query.LocalIdentityPasswordReset,
) error {
	return a.store.ResetLocalIdentityPassword(ctx, pgstatus.LocalIdentityPasswordReset{
		UserID:                 reset.UserID,
		CredentialID:           reset.CredentialID,
		PasswordHash:           reset.PasswordHash,
		PasswordAlgorithm:      reset.PasswordAlgorithm,
		PasswordParametersHash: reset.PasswordParametersHash,
		ResetAt:                reset.ResetAt,
	})
}

// ResetLocalIdentityMFA replaces active MFA factors and recovery hashes.
func (a *corePostgresLocalIdentityAdapter) ResetLocalIdentityMFA(
	ctx context.Context,
	reset query.LocalIdentityMFAReset,
) error {
	return a.store.ResetLocalIdentityMFA(ctx, pgstatus.LocalIdentityMFAReset{
		UserID:              reset.UserID,
		MFAFactorID:         reset.MFAFactorID,
		MFAFactorKind:       reset.MFAFactorKind,
		MFACredentialHandle: reset.MFACredentialHandle,
		RecoveryCodeHashes:  append([]string(nil), reset.RecoveryCodeHashes...),
		ResetAt:             reset.ResetAt,
	})
}

// DisableLocalIdentityUser disables a user and revokes active sessions.
func (a *corePostgresLocalIdentityAdapter) DisableLocalIdentityUser(
	ctx context.Context,
	disable query.LocalIdentityDisableUser,
) error {
	return a.store.DisableLocalIdentityUser(ctx, pgstatus.LocalIdentityDisableUser{
		UserID:     disable.UserID,
		DisabledAt: disable.DisabledAt,
	})
}

// EnableLocalIdentityBreakGlass opens a time-boxed recovery window.
func (a *corePostgresLocalIdentityAdapter) EnableLocalIdentityBreakGlass(
	ctx context.Context,
	window query.LocalIdentityBreakGlassWindow,
) error {
	return a.store.EnableLocalIdentityBreakGlass(ctx, pgstatus.LocalIdentityBreakGlassWindow{
		RecoveryID:         window.RecoveryID,
		TenantID:           window.TenantID,
		WorkspaceID:        window.WorkspaceID,
		SubjectIDHash:      window.SubjectIDHash,
		BreakGlassCodeHash: window.BreakGlassCodeHash,
		Status:             window.Status,
		ReasonCode:         window.ReasonCode,
		PolicyRevisionHash: window.PolicyRevisionHash,
		EnabledAt:          window.EnabledAt,
		ExpiresAt:          window.ExpiresAt,
		CreatedAt:          window.CreatedAt,
		UpdatedAt:          window.UpdatedAt,
	})
}

// ResolveLocalIdentityBreakGlass resolves one time-boxed recovery proof.
func (a *corePostgresLocalIdentityAdapter) ResolveLocalIdentityBreakGlass(
	ctx context.Context,
	attempt query.LocalIdentityBreakGlassAttempt,
) (query.LocalIdentityAuthContext, error) {
	auth, err := a.store.ResolveLocalIdentityBreakGlass(ctx, pgstatus.LocalIdentityBreakGlassAttempt{
		BreakGlassCodeHash: attempt.BreakGlassCodeHash,
		Now:                attempt.Now,
	})
	if err != nil {
		return query.LocalIdentityAuthContext{}, err
	}
	return query.LocalIdentityAuthContext{
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
		SubjectIDHash:      auth.SubjectIDHash,
		SubjectClass:       auth.SubjectClass,
		PolicyRevisionHash: auth.PolicyRevisionHash,
		AllScopes:          auth.AllScopes,
	}, nil
}

// CreateLocalIdentityAPIToken persists a generated API token.
func (a *corePostgresLocalIdentityAdapter) CreateLocalIdentityAPIToken(
	ctx context.Context,
	token query.LocalIdentityAPITokenCreate,
) error {
	return a.store.CreateLocalIdentityAPIToken(ctx, pgstatus.LocalIdentityAPITokenCreate{
		TokenID:            token.TokenID,
		TokenHash:          token.TokenHash,
		TokenClass:         token.TokenClass,
		TenantID:           token.TenantID,
		WorkspaceID:        token.WorkspaceID,
		UserID:             token.UserID,
		ServicePrincipalID: token.ServicePrincipalID,
		DisplayHandleHash:  token.DisplayHandleHash,
		DisplayLabel:       token.DisplayLabel,
		PolicyRevisionHash: token.PolicyRevisionHash,
		IssuedAt:           token.IssuedAt,
		ExpiresAt:          token.ExpiresAt,
	})
}

// RevokeLocalIdentityAPIToken revokes one generated API token.
func (a *corePostgresLocalIdentityAdapter) RevokeLocalIdentityAPIToken(
	ctx context.Context,
	revoke query.LocalIdentityAPITokenRevoke,
) error {
	return translateCoreAPITokenNotFound(a.store.RevokeLocalIdentityAPIToken(ctx, pgstatus.LocalIdentityAPITokenRevoke{
		TokenID:            revoke.TokenID,
		TenantID:           revoke.TenantID,
		WorkspaceID:        revoke.WorkspaceID,
		RevokedAt:          revoke.RevokedAt,
		OwnerSubjectIDHash: revoke.OwnerSubjectIDHash,
	}))
}

// RotateLocalIdentityAPIToken atomically replaces one generated API token.
func (a *corePostgresLocalIdentityAdapter) RotateLocalIdentityAPIToken(
	ctx context.Context,
	rotate query.LocalIdentityAPITokenRotate,
) error {
	return translateCoreAPITokenNotFound(a.store.RotateLocalIdentityAPIToken(ctx, pgstatus.LocalIdentityAPITokenRotate{
		OldTokenID:         rotate.OldTokenID,
		NewTokenID:         rotate.NewTokenID,
		NewTokenHash:       rotate.NewTokenHash,
		TenantID:           rotate.TenantID,
		WorkspaceID:        rotate.WorkspaceID,
		RotatedAt:          rotate.RotatedAt,
		NewTokenExpires:    rotate.NewTokenExpires,
		OwnerSubjectIDHash: rotate.OwnerSubjectIDHash,
	}))
}

// translateCoreAPITokenNotFound maps the storage "no active token" sentinel
// to the query-layer not-found sentinel so self-service revoke/rotate
// surfaces a non-disclosing 404.
func translateCoreAPITokenNotFound(err error) error {
	if errors.Is(err, pgstatus.ErrLocalIdentityAPITokenUnavailable) {
		return query.ErrLocalIdentityAPITokenNotFound
	}
	return err
}

// ListAPITokensBySubject returns metadata-only token rows. It never exposes
// token hashes.
func (a *corePostgresLocalIdentityAdapter) ListAPITokensBySubject(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
) ([]query.LocalIdentityAPITokenListItem, error) {
	items, err := a.store.ListAPITokensBySubject(ctx, subjectIDHash, asOf)
	if err != nil {
		return nil, err
	}
	out := make([]query.LocalIdentityAPITokenListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.LocalIdentityAPITokenListItem{
			TokenID:      item.TokenID,
			TokenClass:   item.TokenClass,
			DisplayLabel: item.DisplayLabel,
			IssuedAt:     item.IssuedAt,
			ExpiresAt:    item.ExpiresAt,
			RevokedAt:    item.RevokedAt,
		})
	}
	return out, nil
}

// GetLocalIdentityMFAStatus returns the safe MFA state. It never exposes
// credential handles or recovery hashes.
func (a *corePostgresLocalIdentityAdapter) GetLocalIdentityMFAStatus(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
) (query.LocalIdentityMFAStatus, error) {
	st, err := a.store.GetLocalIdentityMFAStatus(ctx, subjectIDHash, asOf)
	if err != nil {
		return query.LocalIdentityMFAStatus{}, err
	}
	return query.LocalIdentityMFAStatus{
		HasActiveMFA: st.HasActiveMFA,
		FactorKind:   st.FactorKind,
	}, nil
}

// ResolveLocalIdentityUserID resolves the internal user_id for a session's
// subject hash.
func (a *corePostgresLocalIdentityAdapter) ResolveLocalIdentityUserID(
	ctx context.Context,
	subjectIDHash string,
) (string, bool, error) {
	return a.store.ResolveLocalIdentityUserIDBySubjectHash(ctx, subjectIDHash)
}

// BeginLocalIdentityTOTPEnrollment seals and persists a PENDING TOTP factor.
func (a *corePostgresLocalIdentityAdapter) BeginLocalIdentityTOTPEnrollment(
	ctx context.Context,
	begin query.LocalIdentityTOTPEnrollmentBegin,
) error {
	return a.store.BeginLocalIdentityTOTPEnrollment(ctx, pgstatus.LocalIdentityTOTPEnrollmentBegin{
		UserID:          begin.UserID,
		FactorID:        begin.FactorID,
		SecretPlaintext: begin.SecretPlaintext,
		CreatedAt:       begin.CreatedAt,
	})
}

// ConfirmLocalIdentityTOTPEnrollment verifies the first TOTP code and
// activates the pending factor on match.
func (a *corePostgresLocalIdentityAdapter) ConfirmLocalIdentityTOTPEnrollment(
	ctx context.Context,
	confirm query.LocalIdentityTOTPEnrollmentConfirm,
) error {
	return a.store.ConfirmLocalIdentityTOTPEnrollment(ctx, pgstatus.LocalIdentityTOTPEnrollmentConfirm{
		UserID:   confirm.UserID,
		FactorID: confirm.FactorID,
		Code:     confirm.Code,
		Now:      confirm.Now,
	})
}

// setCoreTOTPSecretKeyring wires the DEK the store uses to seal and open
// TOTP secrets. A nil keyring leaves TOTP enrollment failing closed.
func (a *corePostgresLocalIdentityAdapter) setCoreTOTPSecretKeyring(k *secretcrypto.Keyring) {
	if a == nil || a.store == nil {
		return
	}
	a.store.SetTOTPSecretKeyring(k)
}
