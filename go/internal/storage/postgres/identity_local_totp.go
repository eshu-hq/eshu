// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/totp"
)

// Local identity TOTP sentinel errors (issue #4986). Distinct from the
// recovery-code sentinels in identity_local_types.go so a caller can map
// each to its own API response without inspecting error text.
var (
	// ErrLocalIdentityTOTPKeyringUnavailable means no DEK is configured
	// (SetTOTPSecretKeyring was never called, or
	// ESHU_AUTH_SECRET_ENC_KEY(_FILE) is unset). TOTP enrollment and
	// verification fail closed rather than persist or accept an unsealed
	// secret.
	ErrLocalIdentityTOTPKeyringUnavailable = errors.New("local identity totp secret keyring not configured")
	// ErrLocalIdentityTOTPPendingNotFound means no live pending enrollment
	// matches the (user_id, factor_id) confirm was called with — already
	// confirmed, revoked, or never started.
	ErrLocalIdentityTOTPPendingNotFound = errors.New("local identity totp pending enrollment not found")
	// ErrLocalIdentityTOTPCodeInvalid means a pending enrollment exists but
	// the submitted code did not verify against it. The factor stays
	// pending so the caller may retry.
	ErrLocalIdentityTOTPCodeInvalid = errors.New("local identity totp code invalid")
)

// totpSecretAADPrefix is the AAD scheme+version tag for TOTP secret
// envelopes, mirroring providerSecretAADPrefix's shape
// (identity_provider_config_types.go). Distinct from that prefix so a TOTP
// envelope can never be replayed as a provider-config secret or vice versa
// even though both share the same underlying keyring/DEK.
const totpSecretAADPrefix = "eshu:totp-secret:v1" // #nosec G101 -- AAD scheme/version tag bound into TOTP secret envelopes; not a credential value.

// totpSecretAAD builds the AAD text binding a sealed TOTP secret envelope to
// the exact (user_id, factor_id) row it was sealed for. sealTOTPSecret
// (Seal) and openTOTPSecret (Open) must construct this identically or
// decryption fails closed with secretcrypto.ErrDecrypt — the cut-and-paste
// / confused-deputy defense documented in secretcrypto/README.md.
func totpSecretAAD(userID, factorID string) string {
	return totpSecretAADPrefix + "|" + userID + "|" + factorID
}

// LocalIdentityTOTPEnrollmentBegin starts TOTP enrollment for one user: the
// caller (go/internal/query, via the totp package) generates SecretPlaintext
// and the otpauth:// provisioning URI before calling this method; this
// method's only job is to seal the secret and persist a PENDING factor row.
type LocalIdentityTOTPEnrollmentBegin struct {
	UserID   string
	FactorID string
	// SecretPlaintext is the raw (unsealed) TOTP shared secret. It is sealed
	// immediately inside this call and never returned, logged, or persisted
	// unsealed.
	SecretPlaintext []byte
	CreatedAt       time.Time
}

// LocalIdentityTOTPEnrollmentConfirm verifies the first submitted code
// against a pending enrollment's sealed secret.
type LocalIdentityTOTPEnrollmentConfirm struct {
	UserID   string
	FactorID string
	Code     string
	Now      time.Time
}

// BeginLocalIdentityTOTPEnrollment seals begin.SecretPlaintext under the
// store's TOTP secret keyring (AAD-bound to user_id+factor_id) and inserts
// a PENDING identity_mfa_factors row. The factor cannot satisfy MFA login
// (AuthenticateLocalIdentity, identity_local.go) or count toward
// GetLocalIdentityMFAStatus.HasActiveMFA until
// ConfirmLocalIdentityTOTPEnrollment activates it with a verified first
// code.
func (s *IdentitySubjectStore) BeginLocalIdentityTOTPEnrollment(
	ctx context.Context,
	begin LocalIdentityTOTPEnrollmentBegin,
) error {
	if s.db == nil {
		return errors.New("identity subject store database is required")
	}
	begin.UserID = strings.TrimSpace(begin.UserID)
	begin.FactorID = strings.TrimSpace(begin.FactorID)
	if begin.UserID == "" || begin.FactorID == "" {
		return errors.New("begin local identity totp enrollment requires user_id and factor_id")
	}
	if len(begin.SecretPlaintext) == 0 {
		return errors.New("begin local identity totp enrollment requires secret_plaintext")
	}
	if begin.CreatedAt.IsZero() {
		return errors.New("begin local identity totp enrollment requires created_at")
	}
	sealed, err := s.sealTOTPSecret(begin.UserID, begin.FactorID, begin.SecretPlaintext)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		insertLocalIdentityTOTPFactorQuery,
		begin.FactorID,
		begin.UserID,
		sealed,
		begin.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert pending local identity totp factor: %w", err)
	}
	return nil
}

// ConfirmLocalIdentityTOTPEnrollment verifies confirm.Code against the
// pending factor's sealed secret and, on match, activates the factor.
// Returns ErrLocalIdentityTOTPPendingNotFound when no live pending
// enrollment matches, and ErrLocalIdentityTOTPCodeInvalid when the pending
// enrollment exists but the code did not verify.
func (s *IdentitySubjectStore) ConfirmLocalIdentityTOTPEnrollment(
	ctx context.Context,
	confirm LocalIdentityTOTPEnrollmentConfirm,
) error {
	if s.db == nil {
		return errors.New("identity subject store database is required")
	}
	confirm.UserID = strings.TrimSpace(confirm.UserID)
	confirm.FactorID = strings.TrimSpace(confirm.FactorID)
	confirm.Code = strings.TrimSpace(confirm.Code)
	if confirm.UserID == "" || confirm.FactorID == "" {
		return errors.New("confirm local identity totp enrollment requires user_id and factor_id")
	}
	if confirm.Code == "" {
		return ErrLocalIdentityTOTPCodeInvalid
	}
	if confirm.Now.IsZero() {
		confirm.Now = time.Now().UTC()
	}

	sealed, ok, err := s.selectPendingTOTPSecret(ctx, confirm.UserID, confirm.FactorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLocalIdentityTOTPPendingNotFound
	}
	plaintext, err := s.openTOTPSecret(confirm.UserID, confirm.FactorID, sealed)
	if err != nil {
		return err
	}
	verified, err := totp.Verify(plaintext, confirm.Code, confirm.Now, totp.DefaultStep, totp.DefaultDigits, totp.DefaultSkewSteps)
	if err != nil {
		return fmt.Errorf("verify local identity totp enrollment code: %w", err)
	}
	if !verified {
		return ErrLocalIdentityTOTPCodeInvalid
	}

	result, err := s.db.ExecContext(ctx, activateLocalIdentityTOTPFactorQuery, confirm.UserID, confirm.FactorID, confirm.Now.UTC())
	if err != nil {
		return fmt.Errorf("activate local identity totp factor: %w", err)
	}
	if result != nil {
		if affected, err := result.RowsAffected(); err == nil && affected == 0 {
			// Lost a race with a concurrent confirm/revoke between the select
			// above and this update; report the same not-found the caller
			// already handles rather than silently succeeding.
			return ErrLocalIdentityTOTPPendingNotFound
		}
	}
	return nil
}

// verifyLocalIdentityTOTPCode checks code against every active totp factor
// for userID (see selectLocalIdentityActiveTOTPSecretQuery's doc comment).
// It returns the matching factor id on success — the caller stamps that
// factor's last_used_at — and (false, "", nil), never an error, when no
// active factor verifies, so AuthenticateLocalIdentity treats "wrong TOTP
// code" identically to "wrong recovery code" for failed-attempt accounting.
func (s *IdentitySubjectStore) verifyLocalIdentityTOTPCode(
	ctx context.Context,
	userID string,
	code string,
	now time.Time,
) (bool, string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return false, "", nil
	}
	rows, err := s.db.QueryContext(ctx, selectLocalIdentityActiveTOTPSecretQuery, userID)
	if err != nil {
		return false, "", fmt.Errorf("select active local identity totp secrets: %w", err)
	}
	type factorSecret struct {
		factorID string
		sealed   string
	}
	var factors []factorSecret
	for rows.Next() {
		var fs factorSecret
		if err := rows.Scan(&fs.factorID, &fs.sealed); err != nil {
			_ = rows.Close()
			return false, "", fmt.Errorf("scan active local identity totp secret: %w", err)
		}
		factors = append(factors, fs)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, "", fmt.Errorf("select active local identity totp secrets: %w", err)
	}
	_ = rows.Close()

	for _, fs := range factors {
		plaintext, err := s.openTOTPSecret(userID, fs.factorID, fs.sealed)
		if err != nil {
			// A decrypt failure on one factor (stale DEK, corrupted
			// envelope) must not abort verification of the rest — fail
			// closed on that one factor only, same as a non-matching code.
			continue
		}
		verified, err := totp.Verify(plaintext, code, now, totp.DefaultStep, totp.DefaultDigits, totp.DefaultSkewSteps)
		if err != nil {
			return false, "", fmt.Errorf("verify local identity totp login code: %w", err)
		}
		if verified {
			if _, err := s.db.ExecContext(ctx, touchLocalIdentityTOTPLastUsedQuery, userID, fs.factorID, now.UTC()); err != nil {
				return false, "", fmt.Errorf("touch local identity totp last used: %w", err)
			}
			return true, fs.factorID, nil
		}
	}
	return false, "", nil
}

func (s *IdentitySubjectStore) selectPendingTOTPSecret(ctx context.Context, userID, factorID string) (string, bool, error) {
	rows, err := s.db.QueryContext(ctx, selectLocalIdentityPendingTOTPSecretQuery, userID, factorID)
	if err != nil {
		return "", false, fmt.Errorf("select pending local identity totp secret: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", false, fmt.Errorf("select pending local identity totp secret: %w", err)
		}
		return "", false, nil
	}
	var sealed string
	if err := rows.Scan(&sealed); err != nil {
		return "", false, fmt.Errorf("scan pending local identity totp secret: %w", err)
	}
	return sealed, true, rows.Err()
}

// sealTOTPSecret and openTOTPSecret are the sole Seal/Open call sites for
// TOTP secrets in this codebase, mirroring sealProviderSecret's shape
// (identity_provider_config_writes_helpers.go). AAD binds the envelope to
// the exact (user_id, factor_id) it was sealed for.
func (s *IdentitySubjectStore) sealTOTPSecret(userID, factorID string, plaintext []byte) (string, error) {
	if s.totpSecretKeyring == nil {
		return "", ErrLocalIdentityTOTPKeyringUnavailable
	}
	sealed, err := s.totpSecretKeyring.Seal(plaintext, []byte(totpSecretAAD(userID, factorID)))
	if err != nil {
		return "", fmt.Errorf("seal local identity totp secret: %w", err)
	}
	return sealed, nil
}

func (s *IdentitySubjectStore) openTOTPSecret(userID, factorID, sealed string) ([]byte, error) {
	if s.totpSecretKeyring == nil {
		return nil, ErrLocalIdentityTOTPKeyringUnavailable
	}
	plaintext, err := s.totpSecretKeyring.Open(sealed, []byte(totpSecretAAD(userID, factorID)))
	if err != nil {
		return nil, fmt.Errorf("open local identity totp secret: %w", err)
	}
	return plaintext, nil
}
