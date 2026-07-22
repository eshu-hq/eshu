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
// unrelated MFA method the admin added later. Both the factor revocation
// (revokeBootstrapCredentialRecoveryFactorsQuery, a factor_kind = $2 predicate)
// and the recovery-code revocation (revokeBootstrapCredentialRecoveryCodesQuery,
// a factor_id IN (SELECT ... WHERE factor_kind = $2) subquery) below are scoped
// to localIdentityRecoveryCodeFactorKind: identity_mfa_recovery_codes.factor_id
// is a plain foreign key with no kind constraint of its own, and
// insertLocalIdentityMFA (identity_local_helpers.go) is a shared helper
// ResetLocalIdentityMFA also calls with an operator-supplied mfa_factor_kind
// alongside recovery codes, so a TOTP-kind factor can legitimately own rows
// in identity_mfa_recovery_codes even though TOTP enrollment itself never
// inserts there. Revoking recovery codes unscoped by owning factor_kind would
// silently destroy that TOTP factor's backup codes on every bootstrap
// credential reset (issue #5602 codex review).
func reenrollBootstrapCredentialRecoveryFactor(
	ctx context.Context,
	tx Transaction,
	userID string,
	mfaFactorID string,
	recoveryCodeHash string,
	resetAt time.Time,
) error {
	if _, err := tx.ExecContext(
		ctx, revokeBootstrapCredentialRecoveryCodesQuery, userID, localIdentityRecoveryCodeFactorKind, resetAt,
	); err != nil {
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
		return fmt.Errorf("re-enroll bootstrap credential recovery factor: %w", err)
	}
	return nil
}
