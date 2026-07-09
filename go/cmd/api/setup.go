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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newSetupHandler wires the first-run setup wizard (epic #4962, issue #4965).
// keyring is the same DEK-backed secretcrypto.Keyring used for provider-config
// secrets (providerSecretKeyring in wireAPI) — it is the identical
// ESHU_AUTH_SECRET_ENC_KEY(_FILE) material seed_initial_admin.go seals the
// bootstrap credential envelope with, so no second keyring load is needed. A
// nil keyring (DEK unconfigured) is not fatal here: VerifyBootstrapCredential
// fails closed and SetupNeeded still answers correctly from the row's
// existence alone.
func newSetupHandler(
	db *sql.DB,
	keyring *secretcrypto.Keyring,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
	cookieSecureMode query.CookieSecureMode,
	bootstrapMode string,
) *query.SetupHandler {
	var identityDB pgstorage.ExecQueryer
	if db != nil {
		identityDB = pgstorage.SQLDB{DB: db}
		if instruments != nil {
			identityDB = &pgstorage.InstrumentedDB{
				Inner:       identityDB,
				Tracer:      otel.Tracer(telemetry.DefaultSignalName),
				Instruments: instruments,
				StoreName:   "identity_setup_wizard",
			}
		}
	}
	return &query.SetupHandler{
		Store: &postgresSetupAdapter{
			store:       pgstorage.NewIdentitySubjectStore(identityDB),
			keyring:     keyring,
			instruments: instruments,
		},
		Sessions:      newBrowserSessionStore(db, instruments),
		Audit:         adminRecoveryAuditAppender(governanceAudit),
		Instruments:   instruments,
		CookieSecure:  cookieSecureMode,
		BootstrapMode: bootstrapMode,
	}
}

type postgresSetupAdapter struct {
	store       *pgstorage.IdentitySubjectStore
	keyring     *secretcrypto.Keyring
	instruments *telemetry.Instruments
}

func (a *postgresSetupAdapter) SetupNeeded(ctx context.Context) (bool, error) {
	_, found, err := a.store.SelectBootstrapCredential(ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	if err != nil {
		return false, err
	}
	return found, nil
}

// VerifyBootstrapCredential opens the sealed bootstrap credential envelope
// and compares the submitted plaintext in constant time. It never
// distinguishes "no credential", "already consumed", "wrong key", or "wrong
// password" to the caller — all of them return ok=false, err=nil — so a
// wrong guess against the exposed claim endpoint cannot be used to probe
// instance state.
func (a *postgresSetupAdapter) VerifyBootstrapCredential(ctx context.Context, username, password string) (bool, error) {
	if a.keyring == nil {
		return false, errors.New("bootstrap credential decryption key is not configured")
	}
	cred, found, err := a.store.SelectBootstrapCredential(ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	plaintext, err := a.keyring.Open(cred.SealedCredential, aad)
	recordAuthSecretOpenResult(ctx, a.instruments, err)
	if err != nil {
		return false, nil
	}
	var payload bootstrapCredentialPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return false, nil
	}
	usernameMatch := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(payload.Username)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(payload.Password)) == 1
	return usernameMatch && passwordMatch, nil
}

func (a *postgresSetupAdapter) ResolveSetupOwner(ctx context.Context) (query.SetupOwner, error) {
	userID, subjectIDHash, err := a.store.ResolveBootstrapCredentialOwner(
		ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID,
	)
	if err != nil {
		return query.SetupOwner{}, err
	}
	return query.SetupOwner{
		UserID:        userID,
		SubjectIDHash: subjectIDHash,
		TenantID:      pgstorage.BootstrapAdminTenantID,
		WorkspaceID:   pgstorage.BootstrapAdminWorkspaceID,
	}, nil
}

func (a *postgresSetupAdapter) RotateSetupPassword(ctx context.Context, reset query.LocalIdentityPasswordReset) error {
	return a.store.ResetLocalIdentityPassword(ctx, pgstorage.LocalIdentityPasswordReset{
		UserID:                 reset.UserID,
		CredentialID:           reset.CredentialID,
		PasswordHash:           reset.PasswordHash,
		PasswordAlgorithm:      reset.PasswordAlgorithm,
		PasswordParametersHash: reset.PasswordParametersHash,
		ResetAt:                reset.ResetAt,
	})
}

func (a *postgresSetupAdapter) RotateSetupMFA(ctx context.Context, reset query.LocalIdentityMFAReset) error {
	return a.store.ResetLocalIdentityMFA(ctx, pgstorage.LocalIdentityMFAReset{
		UserID:              reset.UserID,
		MFAFactorID:         reset.MFAFactorID,
		MFAFactorKind:       reset.MFAFactorKind,
		MFACredentialHandle: reset.MFACredentialHandle,
		RecoveryCodeHashes:  append([]string(nil), reset.RecoveryCodeHashes...),
		ResetAt:             reset.ResetAt,
	})
}

func (a *postgresSetupAdapter) CompleteSetup(ctx context.Context, subjectIDHash string, now time.Time) error {
	_, err := a.store.ConsumeBootstrapCredential(
		ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID, subjectIDHash, now,
	)
	return err
}

// recordAuthSecretOpenResult records one eshu_dp_auth_secret_open_total
// observation. It never attaches plaintext, ciphertext, or key material —
// only the bounded operation name and success/error result, mirroring
// recordAuthSecretSealResult in seed_initial_admin_helpers.go.
func recordAuthSecretOpenResult(ctx context.Context, instruments *telemetry.Instruments, err error) {
	if instruments == nil || instruments.AuthSecretOpenTotal == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	instruments.AuthSecretOpenTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, "onetime_admin_credential"),
		attribute.String(telemetry.MetricDimensionResult, result),
	))
}
