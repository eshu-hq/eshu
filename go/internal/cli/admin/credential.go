// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Bootstrap admin credential retrieval and reset (epic #4962, issue #4963),
// behind `eshu admin initial-credential` and
// `eshu admin reset-initial-credential`.
//
// Both operations go straight to Postgres rather than through the API.
// Direct-DB access is the same trust boundary as the API process itself
// (both need the data-encryption key and Postgres credentials to do anything
// useful here); an unauthenticated HTTP endpoint would be new attack
// surface, and a shared-API-key approach has no key to check on a fresh
// stack before the first admin exists.
//
// This package neither opens that connection nor resolves the key: the
// caller passes in an already-open pgstorage.ExecQueryer and an already-
// resolved *secretcrypto.Keyring, because both come from the process
// environment (go/cmd/eshu/admin_initial_credential.go reads
// ESHU_POSTGRES_DSN and hands secretcrypto.KeyringFromEnv its os.Getenv).
// The plaintext credential is returned to that caller; nothing here logs it,
// prints it, or writes it to a file.
const (
	generatedPasswordSize = 24
	generatedRecoverySize = 20
)

// BootstrapCredentialPayload is the plaintext bundle sealed inside the
// bootstrap credential envelope. It mirrors go/cmd/api/seed_initial_admin.go's
// bootstrapCredentialPayload JSON shape; that file is in `package main` and
// cannot be imported, so this struct's field tags must stay byte-for-byte
// identical to the sealing side's.
type BootstrapCredentialPayload struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	RecoveryCode string `json:"recovery_code"`
}

// RetrieveInitialCredential opens the sealed bootstrap admin credential and
// returns its plaintext bundle, appending a durable retrieval audit event
// (success or failure) before it returns either way.
//
// The returned payload carries live secrets. Callers print it once and never
// persist it.
func RetrieveInitialCredential(
	ctx context.Context,
	db pgstorage.ExecQueryer,
	keyring *secretcrypto.Keyring,
) (BootstrapCredentialPayload, error) {
	auditAppender := newAdminCredentialAuditAppender(db)
	store := pgstorage.NewIdentitySubjectStore(db)
	payload, keyID, err := openBootstrapCredentialPayload(ctx, store, keyring)
	auditBootstrapCredentialRetrieved(ctx, auditAppender, keyID, err)
	if err != nil {
		return BootstrapCredentialPayload{}, err
	}
	return payload, nil
}

// ResetInitialCredential regenerates the bootstrap admin credential and
// returns the replacement plaintext bundle, appending a durable reset audit
// event (success or failure) before it returns either way.
//
// username is the operator's override, whitespace-trimmed here. When it is
// blank, the prior credential is opened to carry its username forward; if
// that also fails, the reset is refused rather than guessing a username.
//
// The reset rotates the password AND re-enrolls the MFA recovery-code factor
// atomically (issue #5602), so the returned recovery code actually
// authenticates. It never touches a TOTP factor the admin enrolled after
// bootstrap.
//
// The returned payload carries live secrets. Callers print it once and never
// persist it.
func ResetInitialCredential(
	ctx context.Context,
	db pgstorage.ExecQueryer,
	keyring *secretcrypto.Keyring,
	username string,
) (BootstrapCredentialPayload, error) {
	// The appender exists before any reset work begins, mirroring
	// RetrieveInitialCredential: every return below — including the early
	// username-recovery refusal that runs before a replacement secret is
	// even generated — records exactly one durable reset event.
	auditAppender := newAdminCredentialAuditAppender(db)
	payload, keyID, err := resetInitialCredential(ctx, db, keyring, username)
	auditBootstrapCredentialReset(ctx, auditAppender, keyID, err)
	if err != nil {
		return BootstrapCredentialPayload{}, err
	}
	return payload, nil
}

// resetInitialCredential is ResetInitialCredential's worker. It performs the
// reset and returns the key id its audit event should correlate against: the
// freshly sealed replacement envelope's key id once sealing has succeeded,
// before that the prior envelope's key id when blank-username recovery
// resolved one (which DEK the operator needed but did not have — the same
// correlation a failed retrieval records), and "" when no envelope was ever
// resolved. Never the plaintext password, recovery code, or ciphertext.
//
// It stays unexported so no caller can reach the reset without the audit
// event the exported wrapper appends on every return.
func resetInitialCredential(
	ctx context.Context,
	db pgstorage.ExecQueryer,
	keyring *secretcrypto.Keyring,
	username string,
) (BootstrapCredentialPayload, string, error) {
	store := pgstorage.NewIdentitySubjectStore(db)

	auditKeyID := ""
	username = strings.TrimSpace(username)
	if username == "" {
		existing, priorKeyID, err := openBootstrapCredentialPayload(ctx, store, keyring)
		auditKeyID = priorKeyID
		if err == nil {
			username = existing.Username
		}
	}
	if username == "" {
		return BootstrapCredentialPayload{}, auditKeyID, errors.New(
			"cannot recover the original username (the prior credential was already consumed, reset, or sealed under a different key); pass --username to reset-initial-credential",
		)
	}

	password, err := generateSecret(generatedPasswordSize)
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("generate replacement password: %w", err)
	}
	recoveryCode, err := generateSecret(generatedRecoverySize)
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("generate replacement recovery code: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("hash replacement password: %w", err)
	}

	payload, err := json.Marshal(BootstrapCredentialPayload{ // #nosec G117 -- intentionally marshaling the replacement credential payload immediately before AEAD sealing (keyring.Seal below); the JSON never leaves this function unencrypted
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
	})
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("encode replacement credential payload: %w", err)
	}
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	sealed, err := keyring.Seal(payload, aad)
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("seal replacement credential: %w", err)
	}
	keyID := secretcrypto.EnvelopeKeyID(sealed)
	if keyID == "" {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("resolve sealed credential key id: malformed sealed envelope")
	}
	// From here the replacement envelope is the specific thing the reset is
	// installing, so its key id supersedes the prior envelope's as the
	// audit correlation.
	auditKeyID = keyID

	now := time.Now().UTC()
	// The recovery-code factor is re-enrolled atomically alongside the
	// password rotation and envelope reseal (issue #5602): before this, the
	// returned recovery code was never persisted anywhere, so it could
	// never authenticate. mfaFactorID is a fresh factor row id — a reset
	// always installs a NEW factor rather than reusing the old one, so a
	// concurrent login racing this reset can never observe a factor row with
	// a hash that has not been committed yet.
	mfaFactorID, err := newLocalIdentityFactorID()
	if err != nil {
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("generate replacement mfa factor id: %w", err)
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
	if resetErr != nil {
		if errors.Is(resetErr, pgstorage.ErrBootstrapCredentialNotFound) {
			return BootstrapCredentialPayload{}, auditKeyID, errors.New(
				"no bootstrap credential exists for this deployment (the admin was seeded from ESHU_ADMIN_USERNAME/PASSWORD and has no generated envelope, or ESHU_AUTH_BOOTSTRAP_MODE is sso-only/disabled); there is nothing to reset",
			)
		}
		return BootstrapCredentialPayload{}, auditKeyID, fmt.Errorf("reset bootstrap credential: %w", resetErr)
	}

	return BootstrapCredentialPayload{
		Username:     username,
		Password:     password,
		RecoveryCode: recoveryCode,
	}, auditKeyID, nil
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
) (BootstrapCredentialPayload, string, error) {
	envelope, found, err := store.SelectBootstrapCredential(ctx, pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	if err != nil {
		return BootstrapCredentialPayload{}, "", fmt.Errorf("select bootstrap credential: %w", err)
	}
	if !found {
		return BootstrapCredentialPayload{}, "", errors.New(
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
			return BootstrapCredentialPayload{}, envelope.KeyID, errors.New(
				"cannot decrypt the sealed bootstrap credential: the configured ESHU_AUTH_SECRET_ENC_KEY differs from the key that generated it; run `eshu admin reset-initial-credential` to regenerate the credential under the current key",
			)
		}
		return BootstrapCredentialPayload{}, "", fmt.Errorf("open bootstrap credential: %w", err)
	}

	var payload BootstrapCredentialPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return BootstrapCredentialPayload{}, "", fmt.Errorf("decode bootstrap credential payload: %w", err)
	}
	return payload, envelope.KeyID, nil
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
// newBootstrapID uses for every other bootstrap-identity primary key. That
// file is in `package main` and cannot be imported (see
// BootstrapCredentialPayload's doc comment above), so this is a small
// independent implementation of the same shape.
func newLocalIdentityFactorID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mfa factor id: %w", err)
	}
	return "id_" + hex.EncodeToString(buf), nil
}
