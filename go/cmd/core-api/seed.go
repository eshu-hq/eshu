// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
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

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

// Bootstrap identity seeding (epic #4962). The self-hosted deployment model
// is single-tenant: the bootstrap admin always lands in the fixed
// (tenant_id, workspace_id) slot. There is no per-deployment override env
// pair for it; the env surface is ESHU_ADMIN_USERNAME/PASSWORD[_FILE] and
// ESHU_AUTH_BOOTSTRAP_MODE.
const (
	coreBootstrapAdminDefaultLoginID = "admin"
	coreBootstrapAdminMFAFactorKind  = "recovery_code"

	coreAuthBootstrapModeGenerated = "generated"
	coreAuthBootstrapModeSSOOnly   = "sso-only"
	coreAuthBootstrapModeDisabled  = "disabled"

	coreAuthBootstrapModeEnv       = "ESHU_AUTH_BOOTSTRAP_MODE"
	coreAdminUsernameEnv           = "ESHU_ADMIN_USERNAME"
	coreAdminPasswordEnv           = "ESHU_ADMIN_PASSWORD"      // #nosec G101 -- environment variable name, not a credential
	coreAdminPasswordFileEnv       = "ESHU_ADMIN_PASSWORD_FILE" // #nosec G101 -- environment variable name, not a credential
	coreGeneratedPasswordBytes     = 24
	coreGeneratedRecoveryCodeBytes = 20
)

// SeedCoreAdminBootstrap runs the bootstrap identity seeding stage once per
// process startup, after stores exist and before serving traffic. It never
// re-generates an existing admin: BootstrapLocalIdentity's own
// advisory-locked check-then-insert is the single source of truth, so a
// restart before first login is idempotent by construction.
func SeedCoreAdminBootstrap(
	ctx context.Context,
	identityDB db.ExecQueryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	auditAppender query.GovernanceAuditAppender,
) error {
	tracer := otel.Tracer(telemetry.DefaultSignalName)
	ctx, span := tracer.Start(ctx, "auth.bootstrap_seed")
	defer span.End()

	mode, err := loadCoreAuthBootstrapMode(getenv)
	if err != nil {
		return finishCoreBootstrapSeed(ctx, span, instruments, logger, auditAppender, "error", err)
	}
	store := pgstatus.NewIdentitySubjectStore(identityDB)
	now := time.Now().UTC()

	if username, password, ok := adminCredentialFromCoreEnv(getenv); ok {
		outcome, err := seedCoreBootstrapAdminFromEnv(ctx, store, username, password, now, logger)
		return finishCoreBootstrapSeed(ctx, span, instruments, logger, auditAppender, outcome, err)
	}
	switch mode {
	case coreAuthBootstrapModeSSOOnly, coreAuthBootstrapModeDisabled:
		if logger != nil {
			logger.Info("local admin bootstrap seeding skipped",
				telemetry.EventAttr("auth.bootstrap_seed.skipped"), slog.String("mode", mode))
		}
		return finishCoreBootstrapSeed(ctx, span, instruments, logger, auditAppender, "skipped_"+mode, nil)
	case coreAuthBootstrapModeGenerated:
		outcome, err := seedCoreBootstrapAdminGenerated(ctx, store, getenv, now, logger, instruments, auditAppender)
		return finishCoreBootstrapSeed(ctx, span, instruments, logger, auditAppender, outcome, err)
	default:
		return finishCoreBootstrapSeed(ctx, span, instruments, logger, auditAppender, "error",
			fmt.Errorf("unsupported bootstrap mode %q", mode))
	}
}

// seedCoreBootstrapAdminFromEnv seeds the first local admin from
// ESHU_ADMIN_USERNAME/PASSWORD. It generates and prints a one-time MFA
// recovery code but never seals a bootstrap-credential envelope: the
// operator already holds the password in their own environment.
func seedCoreBootstrapAdminFromEnv(
	ctx context.Context,
	store *pgstatus.IdentitySubjectStore,
	username, password string,
	now time.Time,
	logger *slog.Logger,
) (string, error) {
	recoveryCode, recoveryHash, err := generateCoreBootstrapSecret(coreGeneratedRecoveryCodeBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "error", fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	record := newCoreBootstrapLocalIdentityRecord(username, string(passwordHash), []string{recoveryHash}, now, true)
	if err := store.BootstrapLocalIdentity(ctx, record); err != nil {
		if errors.Is(err, pgstatus.ErrLocalIdentityBootstrapCompleted) {
			logCoreAlreadyProvisioned(logger)
			return "sealed_existing", nil
		}
		return "error", fmt.Errorf("bootstrap local admin from env: %w", err)
	}
	printCoreBootstrapBanner(coreBannerLines{
		Username:     username,
		RecoveryCode: recoveryCode,
		EnvSeeded:    true,
	})
	return "seeded_env", nil
}

// seedCoreBootstrapAdminGenerated seeds the first local admin with a
// crypto/rand-generated password and recovery code, sealing the bundle into
// identity_bootstrap_credentials. It requires a configured DEK: a generated
// admin with no retrievable credential would be a lockout.
func seedCoreBootstrapAdminGenerated(
	ctx context.Context,
	store *pgstatus.IdentitySubjectStore,
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
		logCoreAlreadyProvisioned(logger)
		return "sealed_existing", nil
	}
	keyring, err := secretcrypto.KeyringFromEnv(getenv)
	if err != nil {
		return "error", fmt.Errorf(
			"ESHU_AUTH_BOOTSTRAP_MODE=generated requires a configured data-encryption key: %w", err)
	}
	username := strings.TrimSpace(getenv(coreAdminUsernameEnv))
	if username == "" {
		username = coreBootstrapAdminDefaultLoginID
	}
	password, _, err := generateCoreBootstrapSecret(coreGeneratedPasswordBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap admin password: %w", err)
	}
	recoveryCode, recoveryHash, err := generateCoreBootstrapSecret(coreGeneratedRecoveryCodeBytes)
	if err != nil {
		return "error", fmt.Errorf("generate bootstrap recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "error", fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	record := newCoreBootstrapLocalIdentityRecord(username, string(passwordHash), []string{recoveryHash}, now, false)
	payload, err := json.Marshal(struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		RecoveryCode string `json:"recovery_code"`
	}{Username: username, Password: password, RecoveryCode: recoveryCode})
	if err != nil {
		return "error", fmt.Errorf("encode bootstrap credential payload: %w", err)
	}
	aad := pgstatus.BootstrapCredentialAAD(pgstatus.BootstrapAdminTenantID, pgstatus.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal(payload, aad)
	recordCoreAuthSecretSealResult(ctx, instruments, err)
	if err != nil {
		return "error", fmt.Errorf("seal bootstrap credential: %w", err)
	}
	keyID := secretcrypto.EnvelopeKeyID(sealed)
	if keyID == "" {
		return "error", fmt.Errorf("resolve sealed bootstrap credential key id: malformed sealed envelope")
	}
	inserted, err := store.GenerateBootstrapAdminWithCredential(ctx, record, pgstatus.BootstrapCredentialSeal{
		TenantID:         pgstatus.BootstrapAdminTenantID,
		WorkspaceID:      pgstatus.BootstrapAdminWorkspaceID,
		SubjectIDHash:    record.SubjectIDHash,
		UsernameHash:     query.IdentityHash(username),
		SealedCredential: sealed,
		KeyID:            keyID,
		GeneratedAt:      now,
	})
	if err != nil {
		if errors.Is(err, pgstatus.ErrLocalIdentityBootstrapCompleted) {
			logCoreAlreadyProvisioned(logger)
			return "sealed_existing", nil
		}
		auditCoreBootstrapCredentialGenerated(ctx, auditAppender, keyID, err)
		return "error", fmt.Errorf("bootstrap generated local admin with credential: %w", err)
	}
	auditCoreBootstrapCredentialGenerated(ctx, auditAppender, keyID, nil)
	recordCoreAuthBootstrapCredentialGenerated(ctx, instruments, inserted)
	if !inserted {
		if logger != nil {
			logger.Warn("bootstrap credential row already existed for a freshly bootstrapped admin",
				telemetry.EventAttr("auth.bootstrap_seed.credential_conflict"))
		}
		logCoreAlreadyProvisioned(logger)
		return "generated", nil
	}
	printCoreBootstrapBanner(coreBannerLines{
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
		EnvSeeded:    false,
	})
	return "generated", nil
}

// finishCoreBootstrapSeed records the seed span status and outcome counter,
// returning the original error unchanged.
func finishCoreBootstrapSeed(
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
	auditCoreBootstrapModeChoice(ctx, auditAppender, outcome)
	return err
}

// newCoreBootstrapLocalIdentityRecord builds the hash-only owner record.
// mustChangePassword forces rotation before this credential can obtain a
// session (env-seeded path only; the generated path goes through the setup
// wizard instead).
func newCoreBootstrapLocalIdentityRecord(
	username, passwordHash string,
	recoveryCodeHashes []string,
	now time.Time,
	mustChangePassword bool,
) pgstatus.LocalIdentityBootstrapRecord {
	userID := newCoreBootstrapID()
	factorID := newCoreBootstrapID()
	return pgstatus.LocalIdentityBootstrapRecord{
		TenantID:               pgstatus.BootstrapAdminTenantID,
		WorkspaceID:            pgstatus.BootstrapAdminWorkspaceID,
		UserID:                 userID,
		SubjectIDHash:          query.IdentityHash(username),
		ProfileHandleHash:      query.IdentityHash(username),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: query.IdentityHash("bcrypt"),
		MFAFactorID:            factorID,
		MFAFactorKind:          coreBootstrapAdminMFAFactorKind,
		RecoveryCodeHashes:     recoveryCodeHashes,
		PolicyRevisionHash:     query.IdentityHash(pgstatus.BootstrapAdminTenantID + ":" + pgstatus.BootstrapAdminWorkspaceID),
		CreatedAt:              now,
		MustChangePassword:     mustChangePassword,
	}
}

// loadCoreAuthBootstrapMode reads ESHU_AUTH_BOOTSTRAP_MODE, defaulting to
// generated and failing closed on any value outside the closed enum.
func loadCoreAuthBootstrapMode(getenv func(string) string) (string, error) {
	raw := coreEnvOrEmpty(getenv, coreAuthBootstrapModeEnv)
	if raw == "" {
		return coreAuthBootstrapModeGenerated, nil
	}
	switch raw {
	case coreAuthBootstrapModeGenerated, coreAuthBootstrapModeSSOOnly, coreAuthBootstrapModeDisabled:
		return raw, nil
	default:
		return "", fmt.Errorf(
			"%s=%q is not one of generated, sso-only, disabled", coreAuthBootstrapModeEnv, raw)
	}
}

// LoadCoreAuthBootstrapMode is the main-package entry point for the setup
// wizard's bootstrap-mode posture.
func LoadCoreAuthBootstrapMode(getenv func(string) string) (string, error) {
	return loadCoreAuthBootstrapMode(getenv)
}

// adminCredentialFromCoreEnv reads ESHU_ADMIN_USERNAME plus
// ESHU_ADMIN_PASSWORD/ESHU_ADMIN_PASSWORD_FILE (file takes precedence).
// ok is true only when both a username and a password resolved.
func adminCredentialFromCoreEnv(getenv func(string) string) (username, password string, ok bool) {
	username = coreEnvOrEmpty(getenv, coreAdminUsernameEnv)
	if path := coreEnvOrEmpty(getenv, coreAdminPasswordFileEnv); path != "" {
		if data, err := os.ReadFile(path); err == nil { // #nosec G304 -- path is operator-controlled via env, not request input
			password = strings.TrimSpace(string(data))
		}
	} else {
		password = coreEnvOrEmpty(getenv, coreAdminPasswordEnv)
	}
	return username, password, username != "" && password != ""
}

// generateCoreBootstrapSecret returns a fresh crypto/rand base64url secret
// of n raw bytes plus its query.IdentityHash.
func generateCoreBootstrapSecret(n int) (secret, hash string, err error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate bootstrap secret: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, query.IdentityHash(secret), nil
}

func newCoreBootstrapID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "id_" + hex.EncodeToString(buf[:])
}

// recordCoreAuthSecretSealResult notes one bootstrap-credential seal. The
// retired per-seal metric no longer exists repo-wide, so this stays a
// documented no-op rather than inventing a replacement signal.
func recordCoreAuthSecretSealResult(_ context.Context, _ *telemetry.Instruments, _ error) {
}

// recordCoreAuthBootstrapCredentialGenerated notes one generated-credential
// observation. Same retired-metric rationale as the seal result.
func recordCoreAuthBootstrapCredentialGenerated(_ context.Context, _ *telemetry.Instruments, _ bool) {
}

func logCoreAlreadyProvisioned(logger *slog.Logger) {
	if logger == nil {
		return
	}
	logger.Info(
		"local admin identity already provisioned; retrieve the bootstrap credential with `eshu admin initial-credential`",
		telemetry.EventAttr("auth.bootstrap_seed.already_provisioned"),
	)
}

// coreBannerLines carries the one-time plaintext printed to the startup log.
// This, the `eshu admin initial-credential` CLI, and the operator-managed
// Helm secret are the only three surfaces that ever see this plaintext.
type coreBannerLines struct {
	Username     string
	Password     string
	RecoveryCode string
	EnvSeeded    bool
}

// coreBootstrapBannerWriter is the one-time banner's destination.
// Production always writes to stderr.
var coreBootstrapBannerWriter io.Writer = os.Stderr

func printCoreBootstrapBanner(lines coreBannerLines) {
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
	_, _ = fmt.Fprint(coreBootstrapBannerWriter, b.String())
}
