// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Small, single-purpose helpers for seed_initial_admin.go, split into their
// own file to keep both under the 500-line cap.

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
		SubjectIDHash:          query.IdentityHash(username),
		ProfileHandleHash:      query.IdentityHash(username),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: query.IdentityHash("bcrypt"),
		MFAFactorID:            factorID,
		MFAFactorKind:          bootstrapAdminMFAFactorKind,
		RecoveryCodeHashes:     recoveryCodeHashes,
		PolicyRevisionHash:     query.IdentityHash(pgstorage.BootstrapAdminTenantID + ":" + pgstorage.BootstrapAdminWorkspaceID),
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
		password = strings.TrimSpace(getenv(adminPasswordEnv))
	}
	return username, password, username != "" && password != ""
}

// generateBootstrapSecret returns a fresh crypto/rand base64url secret of n
// raw bytes plus its "sha256:<hex>" hash, via query.IdentityHash — the single
// shared implementation every hash-only identity field this codebase writes
// or compares uses.
func generateBootstrapSecret(n int) (secret, hash string, err error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate bootstrap secret: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, query.IdentityHash(secret), nil
}

func newBootstrapID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "id_" + hex.EncodeToString(buf[:])
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
