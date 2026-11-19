// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// corePostgresSetupAdapter is the core-owned Postgres setup admission
// implementation: bootstrap credential checks over the identity store with
// the shared DEK for envelope opens. A nil keyring fails closed on verify.
type corePostgresSetupAdapter struct {
	store       *pgstatus.IdentitySubjectStore
	keyring     *secretcrypto.Keyring
	instruments *telemetry.Instruments
}

var _ query.SetupAdmission = (*corePostgresSetupAdapter)(nil)

// newCorePostgresSetupAdapter builds the adapter backed by the identity
// subject store.
func newCorePostgresSetupAdapter(
	rawDB *sql.DB,
	keyring *secretcrypto.Keyring,
	instruments *telemetry.Instruments,
) *corePostgresSetupAdapter {
	var identityDB db.ExecQueryer
	if rawDB != nil {
		identityDB = coreInstrumentedDB(rawDB, instruments, "identity_setup_wizard")
	}
	return &corePostgresSetupAdapter{
		store:       pgstatus.NewIdentitySubjectStore(identityDB),
		keyring:     keyring,
		instruments: instruments,
	}
}

// newCoreSetupHandler wires the first-run setup wizard through the versioned
// setup admission contract.
func newCoreSetupHandler(
	setupAdmission query.SetupAdmission,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
	cookieSecureMode query.CookieSecureMode,
	bootstrapMode string,
) *query.SetupHandler {
	setupAdmission = unwrapNilAdmissionPort(setupAdmission)
	return &query.SetupHandler{
		Store:         setupAdmission,
		Sessions:      newCoreBrowserSessionStore(rawDB, instruments),
		Audit:         coreRecoveryAuditAppender(governanceAudit),
		Instruments:   instruments,
		CookieSecure:  cookieSecureMode,
		BootstrapMode: bootstrapMode,
	}
}

// SetupNeeded reports whether the wizard is still reachable: a retrievable
// (unconsumed) bootstrap credential must exist for the fixed slot.
func (a *corePostgresSetupAdapter) SetupNeeded(ctx context.Context) (bool, error) {
	_, found, err := a.store.SelectBootstrapCredential(ctx, pgstatus.BootstrapAdminTenantID, pgstatus.BootstrapAdminWorkspaceID)
	if err != nil {
		return false, err
	}
	return found, nil
}

// VerifyBootstrapCredential checks submitted plaintext against the sealed
// envelope in constant time. Every non-match returns ok=false, err=nil so a
// wrong guess cannot probe instance state.
func (a *corePostgresSetupAdapter) VerifyBootstrapCredential(
	ctx context.Context,
	username, password string,
) (bool, error) {
	if a.keyring == nil {
		return false, errors.New("bootstrap credential decryption key is not configured")
	}
	cred, found, err := a.store.SelectBootstrapCredential(ctx, pgstatus.BootstrapAdminTenantID, pgstatus.BootstrapAdminWorkspaceID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	aad := pgstatus.BootstrapCredentialAAD(pgstatus.BootstrapAdminTenantID, pgstatus.BootstrapAdminWorkspaceID)
	plaintext, err := a.keyring.Open(cred.SealedCredential, aad)
	recordCoreAuthSecretOpenResult(ctx, a.instruments, err)
	if err != nil {
		return false, nil
	}
	return bootstrapCredentialMatches(username, password, plaintext), nil
}

// ResolveSetupOwner resolves the identity the bootstrap credential belongs
// to, for the password/MFA rotation calls that follow verification.
func (a *corePostgresSetupAdapter) ResolveSetupOwner(ctx context.Context) (query.SetupOwner, error) {
	userID, subjectIDHash, err := a.store.ResolveBootstrapCredentialOwner(
		ctx, pgstatus.BootstrapAdminTenantID, pgstatus.BootstrapAdminWorkspaceID,
	)
	if err != nil {
		return query.SetupOwner{}, err
	}
	return query.SetupOwner{
		UserID:        userID,
		SubjectIDHash: subjectIDHash,
		TenantID:      pgstatus.BootstrapAdminTenantID,
		WorkspaceID:   pgstatus.BootstrapAdminWorkspaceID,
	}, nil
}

// RotateSetupPassword replaces the owner's local password.
func (a *corePostgresSetupAdapter) RotateSetupPassword(ctx context.Context, reset query.LocalIdentityPasswordReset) error {
	return a.store.ResetLocalIdentityPassword(ctx, pgstatus.LocalIdentityPasswordReset{
		UserID:                 reset.UserID,
		CredentialID:           reset.CredentialID,
		PasswordHash:           reset.PasswordHash,
		PasswordAlgorithm:      reset.PasswordAlgorithm,
		PasswordParametersHash: reset.PasswordParametersHash,
		ResetAt:                reset.ResetAt,
	})
}

// CompleteSetupMFA atomically rotates MFA and consumes the bootstrap
// credential. completed=false with no error means a concurrent caller won:
// the caller must fail closed, never retry.
func (a *corePostgresSetupAdapter) CompleteSetupMFA(ctx context.Context, in query.CompleteSetupMFAInput) (bool, error) {
	return a.store.CompleteSetupMFA(ctx, pgstatus.CompleteSetupMFAInput{
		TenantID:      in.TenantID,
		WorkspaceID:   in.WorkspaceID,
		SubjectIDHash: in.SubjectIDHash,
		MFA: pgstatus.LocalIdentityMFAReset{
			UserID:              in.UserID,
			MFAFactorID:         in.MFAFactorID,
			MFAFactorKind:       in.MFAFactorKind,
			MFACredentialHandle: in.MFACredentialHandle,
			RecoveryCodeHashes:  append([]string(nil), in.RecoveryCodeHashes...),
			ResetAt:             in.Now,
		},
	})
}

// recordCoreAuthSecretOpenResult notes one bootstrap-envelope open. The
// retired per-open metric no longer exists repo-wide, so this stays a
// documented no-op rather than inventing a replacement signal; the wizard
// funnel itself is covered by eshu_dp_auth_setup_wizard_total.
func recordCoreAuthSecretOpenResult(_ context.Context, _ *telemetry.Instruments, _ error) {
}

// coreBootstrapCredentialPayload is the sealed envelope's plaintext shape:
// the bootstrap username and password, nothing else.
type coreBootstrapCredentialPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// bootstrapCredentialMatches compares submitted plaintext against the sealed
// envelope in constant time. A malformed envelope never matches.
func bootstrapCredentialMatches(username, password string, plaintext []byte) bool {
	var payload coreBootstrapCredentialPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return false
	}
	usernameMatch := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(payload.Username)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(payload.Password)) == 1
	return usernameMatch && passwordMatch
}

// corePostgresSignInPolicyAdapter implements the sign-in policy read and
// mutation ports over the identity subject store.
type corePostgresSignInPolicyAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

var _ query.SignInPolicyAdmission = (*corePostgresSignInPolicyAdapter)(nil)

// newCorePostgresSignInPolicyAdapter builds the adapter, or nil without a
// database.
func newCorePostgresSignInPolicyAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *corePostgresSignInPolicyAdapter {
	if rawDB == nil {
		return nil
	}
	return &corePostgresSignInPolicyAdapter{
		store: pgstatus.NewIdentitySubjectStore(coreInstrumentedDB(rawDB, instruments, "identity_sign_in_policy")),
	}
}

// newCoreSignInPolicyReadHandler wires the policy reads. Nil-safe: a nil
// database yields a store-less handler (503, never panic).
func newCoreSignInPolicyReadHandler(
	database *sql.DB,
	instruments *telemetry.Instruments,
) *query.SignInPolicyReadHandler {
	handler := &query.SignInPolicyReadHandler{}
	if store := newCorePostgresSignInPolicyAdapter(database, instruments); store != nil {
		handler.Store = store
	}
	return handler
}

// newCoreSignInPolicyMutationHandler wires the admin policy write route
// through the versioned admission contract.
func newCoreSignInPolicyMutationHandler(
	admission query.SignInPolicyAdmission,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.SignInPolicyMutationHandler {
	admission = unwrapNilAdmissionPort(admission)
	return &query.SignInPolicyMutationHandler{
		Store:       admission,
		Audit:       coreRecoveryAuditAppender(governanceAudit),
		Instruments: instruments,
	}
}

// GetSignInPolicy reads the tenant policy.
func (a *corePostgresSignInPolicyAdapter) GetSignInPolicy(
	ctx context.Context,
	tenantID string,
) (query.SignInPolicy, error) {
	policy, err := a.store.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		return query.SignInPolicy{}, err
	}
	return signInPolicyFromCorePostgres(policy), nil
}

// UpsertSignInPolicy writes the tenant policy through the guardrail,
// translating storage sentinels to query-layer equivalents.
func (a *corePostgresSignInPolicyAdapter) UpsertSignInPolicy(
	ctx context.Context,
	tenantID string,
	update query.SignInPolicyUpdateRequest,
	policyRevisionHash string,
	now time.Time,
) (query.SignInPolicy, error) {
	policy, err := a.store.UpsertSignInPolicy(ctx, tenantID, pgstatus.SignInPolicyUpdate{
		RequireSSO:             update.RequireSSO,
		AllowLocalUserCreation: update.AllowLocalUserCreation,
		RequireMFAForAllUsers:  update.RequireMFAForAllUsers,
		IdleTimeoutSeconds:     update.IdleTimeoutSeconds,
		AbsoluteTimeoutSeconds: update.AbsoluteTimeoutSeconds,
		PolicyRevisionHash:     policyRevisionHash,
		Now:                    now,
	})
	if err != nil {
		return query.SignInPolicy{}, mapCoreSignInPolicyGuardrailError(err)
	}
	return signInPolicyFromCorePostgres(policy), nil
}

// mapCoreSignInPolicyGuardrailError translates storage guardrail sentinels to
// query-layer equivalents so package query never imports storage/postgres.
func mapCoreSignInPolicyGuardrailError(err error) error {
	switch {
	case errors.Is(err, pgstatus.ErrSignInPolicyGuardrailNoProvenProvider):
		return query.ErrSignInPolicyGuardrailNoProvenProvider
	case errors.Is(err, pgstatus.ErrSignInPolicyGuardrailNoSSOAdminProof):
		return query.ErrSignInPolicyGuardrailNoSSOAdminProof
	case errors.Is(err, pgstatus.ErrSignInPolicyTimeoutOrdering):
		return query.ErrSignInPolicyTimeoutOrdering
	default:
		return err
	}
}

func signInPolicyFromCorePostgres(policy pgstatus.SignInPolicy) query.SignInPolicy {
	return query.SignInPolicy{
		TenantID:                         policy.TenantID,
		RequireSSO:                       policy.RequireSSO,
		AllowLocalUserCreation:           policy.AllowLocalUserCreation,
		RequireMFAForAllUsers:            policy.RequireMFAForAllUsers,
		IdleTimeoutSeconds:               policy.IdleTimeoutSeconds,
		AbsoluteTimeoutSeconds:           policy.AbsoluteTimeoutSeconds,
		SSOAdminVerifiedAt:               policy.SSOAdminVerifiedAt,
		SSOAdminVerifiedProviderConfigID: policy.SSOAdminVerifiedProviderConfigID,
		PolicyRevisionHash:               policy.PolicyRevisionHash,
		UpdatedAt:                        policy.UpdatedAt,
	}
}
