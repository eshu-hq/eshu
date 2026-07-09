// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Bootstrap identity seeding (epic #4962, issue #4963).
//
// Eshu's self-hosted deployment model is single-tenant: the bootstrap admin
// always lands in the fixed (tenant_id, workspace_id) slot
// pgstorage.BootstrapAdminTenantID / pgstorage.BootstrapAdminWorkspaceID.
// There is no per-deployment override env var for this pair (the contract's
// env surface for #4963 is only ESHU_ADMIN_USERNAME/PASSWORD[_FILE] and
// ESHU_AUTH_BOOTSTRAP_MODE).
const (
	bootstrapAdminDefaultLoginID = "admin"
	bootstrapAdminMFAFactorKind  = "recovery_code"

	authBootstrapModeGenerated = "generated"
	authBootstrapModeSSOOnly   = "sso-only"
	authBootstrapModeDisabled  = "disabled"

	authBootstrapModeEnv       = "ESHU_AUTH_BOOTSTRAP_MODE"
	adminUsernameEnv           = "ESHU_ADMIN_USERNAME"
	adminPasswordEnv           = "ESHU_ADMIN_PASSWORD"
	adminPasswordFileEnv       = "ESHU_ADMIN_PASSWORD_FILE"
	generatedPasswordBytes     = 24
	generatedRecoveryCodeBytes = 20
)

// bootstrapCredentialPayload is the plaintext bundle sealed into
// identity_bootstrap_credentials for ESHU_AUTH_BOOTSTRAP_MODE=generated. Its
// JSON encoding is the secretcrypto.Keyring.Seal plaintext input; the sealed
// envelope is the only reversible copy. It is never logged, never attached to
// a span or metric attribute, and appears in cleartext in exactly three
// surfaces: the one-time startup banner below, the `eshu admin
// initial-credential` CLI, and the operator-managed Helm secret it may be
// mirrored into.
type bootstrapCredentialPayload struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	RecoveryCode string `json:"recovery_code"`
}

// seedInitialAdmin runs Eshu's bootstrap identity seeding stage once per API
// process startup, after governanceAudit and before router.Mount (wiring.go).
//
// It never re-generates or invalidates an existing admin identity:
// BootstrapLocalIdentity's own pg_advisory_xact_lock(3455)-guarded
// check-then-insert (identity_local.go) is the single source of truth for
// "has seeding already happened," so a restart before first login is
// idempotent by construction — this function does not perform its own
// existence check up front and instead branches on BootstrapLocalIdentity's
// ErrLocalIdentityBootstrapCompleted result.
//
// AuthenticateLocalIdentity unconditionally requires MFA recovery-code proof
// for every admin-role login (identity_local.go:166-169,
// identity_local_validate.go:41-43), independent of this issue's scope. Both
// the ESHU_ADMIN_USERNAME/PASSWORD-seeded and the generated admin therefore
// need a generated MFA recovery code to complete their first login: this
// stage always prints that one-time recovery code to the startup log.
// ESHU_ADMIN_USERNAME/PASSWORD mode prints only the recovery code (the
// password already came from the operator's own env var and is never sealed
// or re-printed here); ESHU_AUTH_BOOTSTRAP_MODE=generated prints the full
// username/password/recovery-code bundle and additionally seals it into
// identity_bootstrap_credentials for later retrieval via
// `eshu admin initial-credential`.
func seedInitialAdmin(
	ctx context.Context,
	identityDB pgstorage.ExecQueryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	auditAppender query.GovernanceAuditAppender,
) error {
	tracer := otel.Tracer(telemetry.DefaultSignalName)
	ctx, span := tracer.Start(ctx, "auth.bootstrap_seed")
	defer span.End()

	mode, err := loadAuthBootstrapMode(getenv)
	if err != nil {
		return finishBootstrapSeed(ctx, span, instruments, logger, auditAppender, "error", err)
	}

	store := pgstorage.NewIdentitySubjectStore(identityDB)
	now := time.Now().UTC()

	if username, password, ok := adminCredentialFromEnv(getenv); ok {
		outcome, err := seedBootstrapAdminFromEnv(ctx, store, username, password, now, logger)
		return finishBootstrapSeed(ctx, span, instruments, logger, auditAppender, outcome, err)
	}

	switch mode {
	case authBootstrapModeSSOOnly, authBootstrapModeDisabled:
		if logger != nil {
			logger.Info("local admin bootstrap seeding skipped",
				telemetry.EventAttr("auth.bootstrap_seed.skipped"),
				slog.String("mode", mode))
		}
		return finishBootstrapSeed(ctx, span, instruments, logger, auditAppender, "skipped_"+mode, nil)
	case authBootstrapModeGenerated:
		outcome, err := seedBootstrapAdminGenerated(ctx, store, getenv, now, logger, instruments, auditAppender)
		return finishBootstrapSeed(ctx, span, instruments, logger, auditAppender, outcome, err)
	default:
		// Unreachable given loadAuthBootstrapMode's closed enum; fail closed
		// rather than silently falling through to an unhandled mode.
		return finishBootstrapSeed(ctx, span, instruments, logger, auditAppender, "error", fmt.Errorf("unsupported bootstrap mode %q", mode))
	}
}

// seedBootstrapAdminFromEnv seeds the first local admin from
// ESHU_ADMIN_USERNAME/PASSWORD. It always generates and prints a one-time MFA
// recovery code (see seedInitialAdmin's doc comment) but never seals or
// persists a bootstrap-credential envelope: the operator already holds the
// password in their own environment configuration.
func seedBootstrapAdminFromEnv(
	ctx context.Context,
	store *pgstorage.IdentitySubjectStore,
	username, password string,
	now time.Time,
	logger *slog.Logger,
) (string, error) {
	recoveryCode, recoveryHash, err := generateBootstrapSecret(generatedRecoveryCodeBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "error", fmt.Errorf("hash bootstrap admin password: %w", err)
	}

	record := newBootstrapLocalIdentityRecord(username, string(passwordHash), []string{recoveryHash}, now)
	if err := store.BootstrapLocalIdentity(ctx, record); err != nil {
		if errors.Is(err, pgstorage.ErrLocalIdentityBootstrapCompleted) {
			logAlreadyProvisioned(logger)
			return "sealed_existing", nil
		}
		return "error", fmt.Errorf("bootstrap local admin from env: %w", err)
	}

	printBootstrapBanner(bannerLines{
		Username:     username,
		RecoveryCode: recoveryCode,
		EnvSeeded:    true,
	})
	return "seeded_env", nil
}

// seedBootstrapAdminGenerated seeds the first local admin with a
// crypto/rand-generated password and MFA recovery code, sealing the full
// credential bundle into identity_bootstrap_credentials for later retrieval.
// It requires a configured DEK (secretcrypto.KeyringFromEnv): a generated
// admin with no way to ever retrieve the credential is a lockout, so this
// fails closed rather than generating an unretrievable password.
//
// The identity insert and the credential seal are persisted through
// IdentitySubjectStore.GenerateBootstrapAdminWithCredential in one atomic
// transaction, not as two separate calls: a process crash between them would
// otherwise strand an admin identity with no retrievable credential and no
// reset path (ResetBootstrapCredential requires a pre-existing credential
// row to rotate).
//
// HasBootstrappedLocalIdentity runs first as a cheap, lock-free short-circuit
// so a benign restart after the admin already exists does not churn crypto
// (bcrypt hash, AES-GCM seal) or tick eshu_dp_auth_secret_seal_total on every
// boot, and does not require a configured DEK once seeding is already done.
// It is never the correctness boundary — GenerateBootstrapAdminWithCredential's
// own advisory-locked check-then-insert still runs unconditionally afterward
// (the "ON CONFLICT belt"), so a benign race with a concurrent replica's
// startup is still handled correctly.
func seedBootstrapAdminGenerated(
	ctx context.Context,
	store *pgstorage.IdentitySubjectStore,
	getenv func(string) string,
	now time.Time,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
	auditAppender query.GovernanceAuditAppender,
) (string, error) {
	exists, err := store.HasBootstrappedLocalIdentity(ctx)
	if err != nil {
		return "error", fmt.Errorf("check existing local identities: %w", err)
	}
	if exists {
		logAlreadyProvisioned(logger)
		return "sealed_existing", nil
	}

	keyring, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		return "error", fmt.Errorf(
			"ESHU_AUTH_BOOTSTRAP_MODE=generated requires a configured data-encryption key: %w", err,
		)
	}

	username := strings.TrimSpace(getenv(adminUsernameEnv))
	if username == "" {
		username = bootstrapAdminDefaultLoginID
	}
	password, _, err := generateBootstrapSecret(generatedPasswordBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap admin password: %w", err)
	}
	recoveryCode, recoveryHash, err := generateBootstrapSecret(generatedRecoveryCodeBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "error", fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	record := newBootstrapLocalIdentityRecord(username, string(passwordHash), []string{recoveryHash}, now)

	payload, err := json.Marshal(bootstrapCredentialPayload{
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
	})
	if err != nil {
		return "error", fmt.Errorf("encode bootstrap credential payload: %w", err)
	}
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal(payload, aad)
	recordAuthSecretSealResult(ctx, instruments, err)
	if err != nil {
		return "error", fmt.Errorf("seal bootstrap credential: %w", err)
	}
	keyID, err := envelopeKeyID(sealed)
	if err != nil {
		return "error", fmt.Errorf("resolve sealed bootstrap credential key id: %w", err)
	}

	inserted, err := store.GenerateBootstrapAdminWithCredential(ctx, record, pgstorage.BootstrapCredentialSeal{
		TenantID:         pgstorage.BootstrapAdminTenantID,
		WorkspaceID:      pgstorage.BootstrapAdminWorkspaceID,
		SubjectIDHash:    record.SubjectIDHash,
		UsernameHash:     localIdentityHash(username),
		SealedCredential: sealed,
		KeyID:            keyID,
		GeneratedAt:      now,
	})
	if err != nil {
		if errors.Is(err, pgstorage.ErrLocalIdentityBootstrapCompleted) {
			logAlreadyProvisioned(logger)
			return "sealed_existing", nil
		}
		auditBootstrapCredentialGenerated(ctx, auditAppender, keyID, err)
		return "error", fmt.Errorf("bootstrap generated local admin with credential: %w", err)
	}
	auditBootstrapCredentialGenerated(ctx, auditAppender, keyID, nil)
	recordAuthBootstrapCredentialGenerated(ctx, instruments, inserted)
	if !inserted {
		// Extremely unlikely (the identity insert in the same transaction
		// just proved this is a fresh identity), but fail loudly rather than
		// silently discarding a freshly generated, unretrievable credential.
		if logger != nil {
			logger.Warn("bootstrap credential row already existed for a freshly bootstrapped admin",
				telemetry.EventAttr("auth.bootstrap_seed.credential_conflict"))
		}
	}

	printBootstrapBanner(bannerLines{
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
		EnvSeeded:    false,
	})
	return "generated", nil
}

// finishBootstrapSeed records the bootstrap-seed span status and outcome
// counter, and returns the original error unchanged so callers keep normal
// Go error-handling control flow.
func finishBootstrapSeed(
	ctx context.Context,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	auditAppender query.GovernanceAuditAppender,
	outcome string,
	err error,
) error {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if logger != nil {
			logger.Error("bootstrap identity seeding failed",
				telemetry.EventAttr("auth.bootstrap_seed.error"), slog.String("error", err.Error()))
		}
	}
	if instruments != nil && instruments.AuthBootstrapSeedTotal != nil {
		instruments.AuthBootstrapSeedTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionOutcome, outcome),
		))
	}
	auditBootstrapModeChoice(ctx, auditAppender, outcome)
	return err
}
