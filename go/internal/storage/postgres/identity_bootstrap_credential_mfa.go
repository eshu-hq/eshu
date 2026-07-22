// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"
)

// localIdentityRecoveryCodeFactorKind is the identity_mfa_factors.factor_kind
// value ResetBootstrapCredential re-enrolls. It matches go/cmd/api's
// seed_initial_admin.go bootstrapAdminMFAFactorKind constant (the two main
// packages cannot share an unexported constant across binaries — see
// bootstrapCredentialPayloadCLI's doc comment in go/cmd/eshu), so the factor
// this reset installs is indistinguishable in kind from the one bootstrap
// originally seeded.
const localIdentityRecoveryCodeFactorKind = "recovery_code"

// reenrollBootstrapCredentialRecoveryFactor revokes the user's existing
// active recovery-code factor and recovery codes, then installs a fresh
// factor and single recovery-code hash — all inside the caller's open
// transaction, so it commits or rolls back atomically with the password
// rotation and envelope reseal ResetBootstrapCredential performs alongside it
// (issue #5602: the CLI generated and printed a fresh recovery code but never
// persisted it, so only the original first-run code could still
// authenticate; an operator who reset because they lost that original code
// stayed locked out).
//
// This is deliberately narrower than ResetLocalIdentityMFA
// (identity_local_lifecycle.go), the general operator-facing "reset a user's
// MFA" path: that method revokes every active factor regardless of kind.
// ResetBootstrapCredential must never revoke a TOTP factor the admin enrolled
// after bootstrap — restoring the bootstrap credential means restoring the
// password and its original recovery-code factor, not silently discarding an
// unrelated MFA method the admin added later. Reusing
// revokeLocalIdentityRecoveryCodesQuery unscoped by kind is still safe:
// identity_mfa_recovery_codes only ever receives rows from a
// recovery_code-kind factor (TOTP enrollment seals its secret elsewhere and
// never inserts here — see ConfirmLocalIdentityTOTPEnrollment in
// identity_local_totp.go), so revoking every active row for this user_id can
// never reach into TOTP state.
func reenrollBootstrapCredentialRecoveryFactor(
	ctx context.Context,
	tx Transaction,
	userID string,
	mfaFactorID string,
	recoveryCodeHash string,
	resetAt time.Time,
) error {
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityRecoveryCodesQuery, userID, resetAt); err != nil {
		return fmt.Errorf("revoke bootstrap credential recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx, revokeBootstrapCredentialRecoveryFactorsQuery, userID, localIdentityRecoveryCodeFactorKind, resetAt,
	); err != nil {
		return fmt.Errorf("revoke bootstrap credential recovery factor: %w", err)
	}
	if err := insertLocalIdentityMFA(
		ctx, tx, userID, mfaFactorID, localIdentityRecoveryCodeFactorKind, "", []string{recoveryCodeHash}, resetAt,
	); err != nil {
		return err
	}
	return nil
}
