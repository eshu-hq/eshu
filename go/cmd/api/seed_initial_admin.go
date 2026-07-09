// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"

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
) error {
	tracer := otel.Tracer(telemetry.DefaultSignalName)
	ctx, span := tracer.Start(ctx, "auth.bootstrap_seed")
	defer span.End()

	mode, err := loadAuthBootstrapMode(getenv)
	if err != nil {
		return finishBootstrapSeed(ctx, span, instruments, logger, "error", err)
	}

	store := pgstorage.NewIdentitySubjectStore(identityDB)
	now := time.Now().UTC()

	if username, password, ok := adminCredentialFromEnv(getenv); ok {
		outcome, err := seedBootstrapAdminFromEnv(ctx, store, username, password, now, logger)
		return finishBootstrapSeed(ctx, span, instruments, logger, outcome, err)
	}

	switch mode {
	case authBootstrapModeSSOOnly, authBootstrapModeDisabled:
		if logger != nil {
			logger.Info("local admin bootstrap seeding skipped",
				telemetry.EventAttr("auth.bootstrap_seed.skipped"),
				slog.String("mode", mode))
		}
		return finishBootstrapSeed(ctx, span, instruments, logger, "skipped", nil)
	case authBootstrapModeGenerated:
		outcome, err := seedBootstrapAdminGenerated(ctx, store, getenv, now, logger, instruments)
		return finishBootstrapSeed(ctx, span, instruments, logger, outcome, err)
	default:
		// Unreachable given loadAuthBootstrapMode's closed enum; fail closed
		// rather than silently falling through to an unhandled mode.
		return finishBootstrapSeed(ctx, span, instruments, logger, "error", fmt.Errorf("unsupported bootstrap mode %q", mode))
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
func seedBootstrapAdminGenerated(
	ctx context.Context,
	store *pgstorage.IdentitySubjectStore,
	getenv func(string) string,
	now time.Time,
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) (string, error) {
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
		return "error", fmt.Errorf("bootstrap generated local admin with credential: %w", err)
	}
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

// newBootstrapLocalIdentityRecord builds the hash-only owner record
// BootstrapLocalIdentity persists. It always installs exactly one MFA
// recovery-code hash: AuthenticateLocalIdentity requires MFA proof for every
// admin-role login regardless of how the admin was seeded (see
// seedInitialAdmin's doc comment).
func newBootstrapLocalIdentityRecord(
	username, passwordHash string,
	recoveryCodeHashes []string,
	now time.Time,
) pgstorage.LocalIdentityBootstrapRecord {
	userID := newBootstrapID()
	factorID := newBootstrapID()
	return pgstorage.LocalIdentityBootstrapRecord{
		TenantID:               pgstorage.BootstrapAdminTenantID,
		WorkspaceID:            pgstorage.BootstrapAdminWorkspaceID,
		UserID:                 userID,
		SubjectIDHash:          localIdentityHash(username),
		ProfileHandleHash:      localIdentityHash(username),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: localIdentityHash("bcrypt"),
		MFAFactorID:            factorID,
		MFAFactorKind:          bootstrapAdminMFAFactorKind,
		RecoveryCodeHashes:     recoveryCodeHashes,
		PolicyRevisionHash:     localIdentityHash(pgstorage.BootstrapAdminTenantID + ":" + pgstorage.BootstrapAdminWorkspaceID),
		CreatedAt:              now,
	}
}

// loadAuthBootstrapMode reads ESHU_AUTH_BOOTSTRAP_MODE, defaulting to
// "generated" and failing closed on any value outside the closed enum
// (generated, sso-only, disabled).
func loadAuthBootstrapMode(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv(authBootstrapModeEnv))
	if raw == "" {
		return authBootstrapModeGenerated, nil
	}
	switch raw {
	case authBootstrapModeGenerated, authBootstrapModeSSOOnly, authBootstrapModeDisabled:
		return raw, nil
	default:
		return "", fmt.Errorf(
			"%s=%q is not one of generated, sso-only, disabled", authBootstrapModeEnv, raw,
		)
	}
}

// adminCredentialFromEnv reads ESHU_ADMIN_USERNAME plus
// ESHU_ADMIN_PASSWORD/ESHU_ADMIN_PASSWORD_FILE (file takes precedence over
// inline, mirroring secretcrypto's DEK-loading precedence). ok is true only
// when both a username and a password resolved, matching the boot decision's
// conjunctive "ESHU_ADMIN_USERNAME + password" condition.
func adminCredentialFromEnv(getenv func(string) string) (username, password string, ok bool) {
	username = strings.TrimSpace(getenv(adminUsernameEnv))
	if path := strings.TrimSpace(getenv(adminPasswordFileEnv)); path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- path is operator-controlled via ESHU_ADMIN_PASSWORD_FILE, not request input
		if err == nil {
			password = strings.TrimSpace(string(data))
		}
	} else {
		password = getenv(adminPasswordEnv)
	}
	return username, password, username != "" && password != ""
}

// generateBootstrapSecret returns a fresh crypto/rand base64url secret of n
// raw bytes plus its "sha256:<hex>" hash, matching the hashing convention
// go/internal/query/local_identity_handler_helpers.go's localIdentityHash
// uses for every other hash-only identity field.
func generateBootstrapSecret(n int) (secret, hash string, err error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate bootstrap secret: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, localIdentityHash(secret), nil
}

// localIdentityHash mirrors go/internal/query/local_identity_handler_helpers.go's
// unexported localIdentityHash so every hash-only identity field this package
// writes (subject_id_hash, profile_handle_hash, recovery-code hashes, policy
// revision hash) uses the identical "sha256:<hex>" convention the rest of the
// local-identity surface reads.
func localIdentityHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newBootstrapID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "id_" + hex.EncodeToString(buf[:])
}

// envelopeKeyID extracts the key_id field secretcrypto.Keyring.Seal embedded
// in a sealed ESK1 envelope (format "ESK1.<key_id>.<nonce>.<ciphertext>"), so
// GenerateBootstrapCredential's key_id column always matches the envelope
// without this package re-deriving secretcrypto's private fingerprint rule.
func envelopeKeyID(sealed string) (string, error) {
	parts := strings.SplitN(sealed, ".", 4)
	if len(parts) != 4 || parts[1] == "" {
		return "", fmt.Errorf("malformed sealed envelope")
	}
	return parts[1], nil
}

// recordAuthSecretSealResult records one eshu_dp_auth_secret_seal_total
// observation for the bootstrap-credential operation. It never attaches
// plaintext, ciphertext, or key material — only the bounded operation name
// and success/error result.
func recordAuthSecretSealResult(ctx context.Context, instruments *telemetry.Instruments, err error) {
	if instruments == nil || instruments.AuthSecretSealTotal == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	instruments.AuthSecretSealTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, "onetime_admin_credential"),
		attribute.String(telemetry.MetricDimensionResult, result),
	))
}

// recordAuthBootstrapCredentialGenerated records one
// eshu_dp_auth_bootstrap_credential_generated_total observation.
func recordAuthBootstrapCredentialGenerated(ctx context.Context, instruments *telemetry.Instruments, inserted bool) {
	if instruments == nil || instruments.AuthBootstrapCredentialGeneratedTotal == nil {
		return
	}
	result := "already_provisioned"
	if inserted {
		result = "generated"
	}
	instruments.AuthBootstrapCredentialGeneratedTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionResult, result),
	))
}

func logAlreadyProvisioned(logger *slog.Logger) {
	if logger == nil {
		return
	}
	logger.Info(
		"local admin identity already provisioned; retrieve the bootstrap credential with `eshu admin initial-credential`",
		telemetry.EventAttr("auth.bootstrap_seed.already_provisioned"),
	)
}

// bannerLines carries the one-time plaintext printed to the startup log.
// This, the `eshu admin initial-credential` CLI, and the operator-managed
// Helm secret it may be mirrored into are the only three surfaces that ever
// see this plaintext (epic #4962's negative-leakage invariant).
type bannerLines struct {
	Username     string
	Password     string
	RecoveryCode string
	EnvSeeded    bool
}

// bootstrapBannerWriter is the one-time banner's destination. Production
// always writes to stderr; tests swap this to a buffer so the negative-
// leakage proof can assert the plaintext appears exactly once, only here.
var bootstrapBannerWriter io.Writer = os.Stderr

func printBootstrapBanner(lines bannerLines) {
	var b strings.Builder
	b.WriteString("\n================ ESHU BOOTSTRAP ADMIN CREDENTIAL (one-time) ================\n")
	fmt.Fprintf(&b, "username:      %s\n", lines.Username)
	if !lines.EnvSeeded {
		fmt.Fprintf(&b, "password:      %s\n", lines.Password)
	}
	fmt.Fprintf(&b, "recovery code: %s\n", lines.RecoveryCode)
	if lines.EnvSeeded {
		b.WriteString("(password was set via ESHU_ADMIN_PASSWORD; only the MFA recovery code is generated)\n")
	} else {
		b.WriteString("Retrieve this again with: eshu admin initial-credential\n")
	}
	b.WriteString("This banner will not be shown again. Save these values now.\n")
	b.WriteString("==============================================================================\n")
	_, _ = fmt.Fprint(bootstrapBannerWriter, b.String())
}

// finishBootstrapSeed records the bootstrap-seed span status and outcome
// counter, and returns the original error unchanged so callers keep normal
// Go error-handling control flow.
func finishBootstrapSeed(
	ctx context.Context,
	span trace.Span,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
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
	return err
}
