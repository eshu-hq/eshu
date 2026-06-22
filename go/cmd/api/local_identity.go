package main

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type postgresLocalIdentityAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

func newLocalIdentityHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.LocalIdentityHandler {
	handler := &query.LocalIdentityHandler{
		Audit: adminRecoveryAuditAppender(governanceAudit),
	}
	if store := newPostgresLocalIdentityAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	handler.Sessions = newBrowserSessionStore(db, instruments)
	return handler
}

func newPostgresLocalIdentityAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresLocalIdentityAdapter {
	if db == nil {
		return nil
	}
	identityDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		identityDB = &pgstatus.InstrumentedDB{
			Inner:       identityDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "identity_subjects",
		}
	}
	return &postgresLocalIdentityAdapter{store: pgstatus.NewIdentitySubjectStore(identityDB)}
}

func (a *postgresLocalIdentityAdapter) BootstrapLocalIdentity(
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

func (a *postgresLocalIdentityAdapter) AuthenticateLocalIdentity(
	ctx context.Context,
	attempt query.LocalIdentityAuthenticationAttempt,
) (query.LocalIdentityAuthenticationResult, error) {
	result, err := a.store.AuthenticateLocalIdentity(ctx, pgstatus.LocalIdentityAuthenticationAttempt{
		SubjectIDHash:         attempt.SubjectIDHash,
		Password:              attempt.Password,
		MFARecoveryCodeHash:   attempt.MFARecoveryCodeHash,
		ConsumeRecoveryCodeAt: attempt.ConsumeRecoveryCodeAt,
		Now:                   attempt.Now,
	})
	if err != nil {
		return query.LocalIdentityAuthenticationResult{}, err
	}
	return query.LocalIdentityAuthenticationResult{
		Status:        query.LocalIdentityAuthStatus(result.Status),
		Authenticated: result.Authenticated,
		Auth: query.LocalIdentityAuthContext{
			TenantID:           result.Auth.TenantID,
			WorkspaceID:        result.Auth.WorkspaceID,
			SubjectIDHash:      result.Auth.SubjectIDHash,
			SubjectClass:       result.Auth.SubjectClass,
			PolicyRevisionHash: result.Auth.PolicyRevisionHash,
			AllScopes:          result.Auth.AllScopes,
		},
		LockedUntil: result.LockedUntil,
	}, nil
}

func (a *postgresLocalIdentityAdapter) CreateLocalIdentityInvitation(
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

func (a *postgresLocalIdentityAdapter) AcceptLocalIdentityInvitation(
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

func (a *postgresLocalIdentityAdapter) ResetLocalIdentityPassword(
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

func (a *postgresLocalIdentityAdapter) ResetLocalIdentityMFA(
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

func (a *postgresLocalIdentityAdapter) DisableLocalIdentityUser(
	ctx context.Context,
	disable query.LocalIdentityDisableUser,
) error {
	return a.store.DisableLocalIdentityUser(ctx, pgstatus.LocalIdentityDisableUser{
		UserID:     disable.UserID,
		DisabledAt: disable.DisabledAt,
	})
}

func (a *postgresLocalIdentityAdapter) EnableLocalIdentityBreakGlass(
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

func (a *postgresLocalIdentityAdapter) ResolveLocalIdentityBreakGlass(
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
