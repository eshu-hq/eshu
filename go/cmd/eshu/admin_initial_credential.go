// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// eshu admin initial-credential / reset-initial-credential (epic #4962,
// issue #4963).
//
// Both subcommands connect directly to Postgres (sql.Open("pgx", dsn) from
// ESHU_POSTGRES_DSN, the existing cmd/eshu pattern at
// local_host_config.go:227) rather than calling the API. Direct-DB access is
// the same trust boundary as the API process itself (both need the DEK and
// Postgres credentials to do anything useful here); an unauthenticated HTTP
// endpoint would be new attack surface, and a shared-API-key approach has no
// key to check on a fresh stack before the first admin exists.
//
// The generated plaintext is printed to stdout exactly once per invocation
// and is never logged or written to any file by this command.
const (
	adminCredentialDSNEnv = "ESHU_POSTGRES_DSN" // #nosec G101 -- environment variable name, not a credential
	generatedPasswordSize = 24
	generatedRecoverySize = 20
)

// bootstrapCredentialPayloadCLI mirrors go/cmd/api/seed_initial_admin.go's
// bootstrapCredentialPayload JSON shape. The two packages cannot share an
// unexported type across binaries, so this struct's field tags must stay
// byte-for-byte identical to the sealing side's.
type bootstrapCredentialPayloadCLI struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	RecoveryCode string `json:"recovery_code"`
}

func init() {
	initialCredentialCmd := &cobra.Command{
		Use:   "initial-credential",
		Short: "Retrieve the one-time generated bootstrap admin credential",
		RunE:  runAdminInitialCredential,
	}
	adminCmd.AddCommand(initialCredentialCmd)

	resetInitialCredentialCmd := &cobra.Command{
		Use:   "reset-initial-credential",
		Short: "Regenerate and reseal the bootstrap admin credential",
		Long: "reset-initial-credential atomically rotates the bootstrap admin's " +
			"password AND re-enrolls its MFA recovery-code factor (issue #5602), " +
			"so the printed recovery code below actually authenticates. It never " +
			"touches a TOTP factor the admin enrolled after bootstrap. Use this " +
			"when the original one-time credential was lost, expired under the " +
			"configured data-encryption key, or already consumed.",
		RunE: runAdminResetInitialCredential,
	}
	resetInitialCredentialCmd.Flags().String("username", "", "Username to seal into the new credential bundle; required only if the prior credential cannot be recovered to carry it forward")
	adminCmd.AddCommand(resetInitialCredentialCmd)
}

func runAdminInitialCredential(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	db, err := openAdminCredentialDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	keyring, err := secretcrypto.KeyringFromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve data-encryption key: %w", err)
	}

	auditAppender := newAdminCredentialAuditAppender(pgstorage.SQLDB{DB: db})
	store := pgstorage.NewIdentitySubjectStore(pgstorage.SQLDB{DB: db})
	payload, keyID, err := openBootstrapCredentialPayload(ctx, store, keyring)
	auditBootstrapCredentialRetrieved(ctx, auditAppender, keyID, err)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"username:      %s\npassword:      %s\nrecovery code: %s\n",
		payload.Username, payload.Password, payload.RecoveryCode)
	return nil
}

func runAdminResetInitialCredential(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	db, err := openAdminCredentialDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	keyring, err := secretcrypto.KeyringFromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("resolve data-encryption key: %w", err)
	}
	store := pgstorage.NewIdentitySubjectStore(pgstorage.SQLDB{DB: db})

	username, _ := cmd.Flags().GetString("username")
	username = strings.TrimSpace(username)
	if username == "" {
		if existing, _, err := openBootstrapCredentialPayload(ctx, store, keyring); err == nil {
			username = existing.Username
		}
	}
	if username == "" {
		return errors.New(
			"cannot recover the original username (the prior credential was already consumed, reset, or sealed under a different key); pass --username to reset-initial-credential",
		)
	}

	password, err := generateSecret(generatedPasswordSize)
	if err != nil {
		return fmt.Errorf("generate replacement password: %w", err)
	}
	recoveryCode, err := generateSecret(generatedRecoverySize)
	if err != nil {
		return fmt.Errorf("generate replacement recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash replacement password: %w", err)
	}

	payload, err := json.Marshal(bootstrapCredentialPayloadCLI{ // #nosec G117 -- intentionally marshaling the replacement credential payload immediately before AEAD sealing (keyring.Seal below); the JSON never leaves this function unencrypted
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
	})
	if err != nil {
		return fmt.Errorf("encode replacement credential payload: %w", err)
	}
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal(payload, aad)
	if err != nil {
		return fmt.Errorf("seal replacement credential: %w", err)
	}
	keyID := secretcrypto.EnvelopeKeyID(sealed)
	if keyID == "" {
		return fmt.Errorf("resolve sealed credential key id: malformed sealed envelope")
	}

	now := time.Now().UTC()
	// The recovery-code factor is re-enrolled atomically alongside the
	// password rotation and envelope reseal (issue #5602): before this, the
	// printed recovery code below was never persisted anywhere, so it could
	// never authenticate. mfaFactorID is a fresh factor row id — a reset
	// always installs a NEW factor rather than reusing the old one, so a
	// concurrent login racing this reset can never observe a factor row with
	// a hash that has not been committed yet.
	mfaFactorID, err := newLocalIdentityFactorID()
	if err != nil {
		return fmt.Errorf("generate replacement mfa factor id: %w", err)
	}
	recoveryCodeHash := query.IdentityHash(recoveryCode)
	resetErr := store.ResetBootstrapCredential(ctx, pgstorage.ResetBootstrapCredentialInput{
		TenantID:               pgstorage.BootstrapAdminTenantID,
		WorkspaceID:            pgstorage.BootstrapAdminWorkspaceID,
		SealedCredential:       sealed,
		KeyID:                  keyID,
		PasswordHash:           string(passwordHash),
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: query.IdentityHash("bcrypt"),
		MFAFactorID:            mfaFactorID,
		RecoveryCodeHash:       recoveryCodeHash,
		ResetAt:                now,
	})
	auditAppender := newAdminCredentialAuditAppender(pgstorage.SQLDB{DB: db})
	auditBootstrapCredentialReset(ctx, auditAppender, keyID, resetErr)
	if resetErr != nil {
		if errors.Is(resetErr, pgstorage.ErrBootstrapCredentialNotFound) {
			return errors.New(
				"no bootstrap credential exists for this deployment (the admin was seeded from ESHU_ADMIN_USERNAME/PASSWORD and has no generated envelope, or ESHU_AUTH_BOOTSTRAP_MODE is sso-only/disabled); there is nothing to reset",
			)
		}
		return fmt.Errorf("reset bootstrap credential: %w", resetErr)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"username:      %s\npassword:      %s\nrecovery code: %s\n",
		username, password, recoveryCode)
	return nil
}

// openBootstrapCredentialPayload retrieves and opens the sealed bootstrap
// credential envelope, returning an actionable error on decrypt failure
// rather than a bare secretcrypto.ErrDecrypt. The returned keyID (safe to
// record: epic #4962 "key_id OK on spans/logs", never the plaintext
// credential) lets callers correlate a durable audit event with the
// specific envelope that was opened.
func openBootstrapCredentialPayload(
	ctx context.Context,
	store *pgstorage.IdentitySubjectStore,
	keyring *secretcrypto.Keyring,
) (bootstrapCredentialPayloadCLI, string, error) {
	envelope, found, err := store.SelectBootstrapCredential(ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	if err != nil {
		return bootstrapCredentialPayloadCLI{}, "", fmt.Errorf("select bootstrap credential: %w", err)
	}
	if !found {
		return bootstrapCredentialPayloadCLI{}, "", errors.New(
			"no retrievable bootstrap credential: it was already consumed by a login, never generated (check ESHU_AUTH_BOOTSTRAP_MODE), or already reset; run `eshu admin reset-initial-credential` to regenerate one",
		)
	}

	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	plaintext, err := keyring.Open(envelope.SealedCredential, aad)
	if err != nil {
		if errors.Is(err, secretcrypto.ErrDecrypt) {
			// envelope.KeyID is already known from the SELECT above (Open
			// never needed it to fail this way), so a failed-retrieval audit
			// event can still correlate to which DEK the caller needed but
			// didn't have.
			return bootstrapCredentialPayloadCLI{}, envelope.KeyID, errors.New(
				"cannot decrypt the sealed bootstrap credential: the configured ESHU_AUTH_SECRET_ENC_KEY differs from the key that generated it; run `eshu admin reset-initial-credential` to regenerate the credential under the current key",
			)
		}
		return bootstrapCredentialPayloadCLI{}, "", fmt.Errorf("open bootstrap credential: %w", err)
	}

	var payload bootstrapCredentialPayloadCLI
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return bootstrapCredentialPayloadCLI{}, "", fmt.Errorf("decode bootstrap credential payload: %w", err)
	}
	return payload, envelope.KeyID, nil
}

// openAdminCredentialDB opens a direct Postgres connection from
// ESHU_POSTGRES_DSN, mirroring local_host_config.go:227's
// applyLocalBootstrap pattern.
func openAdminCredentialDB(ctx context.Context) (*sql.DB, error) {
	dsn := strings.TrimSpace(os.Getenv(adminCredentialDSNEnv))
	if dsn == "" {
		return nil, fmt.Errorf("%s is required", adminCredentialDSNEnv)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres connection: %w", err)
	}
	return db, nil
}

// generateSecret returns a fresh crypto/rand base64url secret of n raw bytes.
func generateSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// newLocalIdentityFactorID returns a fresh opaque MFA factor identifier for
// the recovery-code factor a reset re-enrolls (issue #5602), matching the
// "id_<32 hex chars>" shape go/cmd/api/seed_initial_admin_helpers.go's
// newBootstrapID uses for every other bootstrap-identity primary key. The two
// main packages cannot share an unexported helper across binaries (see
// bootstrapCredentialPayloadCLI's doc comment above), so this is a small
// independent implementation of the same shape.
func newLocalIdentityFactorID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mfa factor id: %w", err)
	}
	return "id_" + hex.EncodeToString(buf), nil
}
