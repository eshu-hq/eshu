// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CompleteSetupMFAInput carries the caller-hashed recovery codes and owner
// identity the first-run setup wizard's final step (#4965, #4990) needs to
// atomically rotate MFA and consume the bootstrap credential.
type CompleteSetupMFAInput struct {
	TenantID      string
	WorkspaceID   string
	SubjectIDHash string
	MFA           LocalIdentityMFAReset
}

// CompleteSetupMFA atomically rotates the wizard owner's MFA recovery-code
// factor and permanently consumes the bootstrap credential envelope in one
// transaction guarded by pg_advisory_xact_lock(3456) — the same lock
// GenerateBootstrapCredential/ResetBootstrapCredential/insertBootstrapCredentialInTx
// already use for this table (identity_bootstrap_credential_sql.go). Two
// concurrent completions for the same (tenant, workspace, subject) therefore
// serialize on the lock instead of both rotating MFA and both believing they
// sealed the wizard (#4990 P1: the prior design ran RotateSetupMFA and
// CompleteSetup as two separate, unguarded store calls).
//
// completed is false, with no error, in two cases:
//   - a concurrent completion already consumed the credential inside this
//     same critical section before this caller acquired the lock (detected
//     by selectBootstrapCredentialConsumedState under the lock); or
//   - the final consume UPDATE affects zero rows despite the check above
//     (defensive: should be unreachable given the same-transaction check,
//     but this must never commit a half-applied state).
//
// In both cases the transaction rolls back untouched — no MFA factor or
// recovery code row is ever written for a losing caller, so its generated
// codes are never persisted (never orphaned) and the caller MUST discard
// them and fail closed rather than issue a session.
func (s *IdentitySubjectStore) CompleteSetupMFA(
	ctx context.Context,
	in CompleteSetupMFAInput,
) (bool, error) {
	in = normalizeCompleteSetupMFAInput(in)
	if err := validateCompleteSetupMFAInput(in); err != nil {
		return false, err
	}
	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, bootstrapCredentialLockQuery); err != nil {
		return false, fmt.Errorf("lock bootstrap credential: %w", err)
	}
	consumed, err := selectBootstrapCredentialConsumedState(ctx, tx, in.TenantID, in.WorkspaceID, in.SubjectIDHash)
	if err != nil {
		return false, err
	}
	if consumed {
		// A concurrent completion already won the race inside this same
		// advisory-locked critical section. Roll back without touching MFA
		// — there is nothing to orphan because nothing was written yet.
		return false, nil
	}

	mfa := in.MFA
	// Serialize the recovery-factor rotation below against any concurrent
	// ResetLocalIdentityMFA for the same user. Like ResetBootstrapCredential's
	// re-enroll path, this revoke-then-insert of an active recovery_code factor
	// has no unique "one active factor per user" constraint to fall back on, so
	// without a shared lock a concurrent ResetLocalIdentityMFA (which takes only
	// the per-user key, never 3456) can interleave and leave two simultaneously
	// active recovery-code factors (identity_local_mfa_reset_lock.go documents
	// the hazard). Taken AFTER 3456, preserving the acyclic
	// 3455 -> 3456 -> per-user lock hierarchy.
	if err := lockLocalIdentityMFAReset(ctx, tx, mfa.UserID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityRecoveryCodesQuery, mfa.UserID, mfa.ResetAt); err != nil {
		return false, fmt.Errorf("revoke local identity recovery codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, revokeLocalIdentityMFAFactorsQuery, mfa.UserID, mfa.ResetAt); err != nil {
		return false, fmt.Errorf("revoke local identity mfa factors: %w", err)
	}
	if err := insertLocalIdentityMFA(
		ctx, tx, mfa.UserID, mfa.MFAFactorID, mfa.MFAFactorKind, mfa.MFACredentialHandle, mfa.RecoveryCodeHashes, mfa.ResetAt,
	); err != nil {
		return false, err
	}

	result, err := tx.ExecContext(ctx, consumeBootstrapCredentialQuery, in.TenantID, in.WorkspaceID, in.SubjectIDHash, mfa.ResetAt)
	if err != nil {
		return false, fmt.Errorf("consume bootstrap credential: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume bootstrap credential: %w", err)
	}
	if affected == 0 {
		// Unreachable in practice given the consumed check above ran inside
		// the same transaction under the same lock, but fail closed rather
		// than commit MFA rotation without the matching consume.
		return false, nil
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit complete setup mfa: %w", err)
	}
	committed = true
	return true, nil
}

// selectBootstrapCredentialConsumedState reports whether the bootstrap
// credential row for (tenant, workspace, subject) is already consumed. A
// missing row is treated as "already consumed" (fails closed) since
// CompleteSetupMFA's caller only ever calls this after already resolving an
// owner from an existing row in the same request.
func selectBootstrapCredentialConsumedState(
	ctx context.Context,
	db ExecQueryer,
	tenantID, workspaceID, subjectIDHash string,
) (bool, error) {
	rows, err := db.QueryContext(ctx, selectBootstrapCredentialConsumedStateQuery, tenantID, workspaceID, subjectIDHash)
	if err != nil {
		return false, fmt.Errorf("select bootstrap credential consumed state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("select bootstrap credential consumed state: %w", err)
		}
		return true, nil
	}
	var consumed bool
	if err := rows.Scan(&consumed); err != nil {
		return false, fmt.Errorf("select bootstrap credential consumed state: %w", err)
	}
	return consumed, rows.Err()
}

func normalizeCompleteSetupMFAInput(in CompleteSetupMFAInput) CompleteSetupMFAInput {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.SubjectIDHash = strings.TrimSpace(in.SubjectIDHash)
	in.MFA = normalizeMFAReset(in.MFA)
	return in
}

func validateCompleteSetupMFAInput(in CompleteSetupMFAInput) error {
	if in.TenantID == "" || in.WorkspaceID == "" || in.SubjectIDHash == "" {
		return errors.New("complete setup mfa input requires tenant_id, workspace_id, and subject_id_hash")
	}
	return validateMFAReset(in.MFA)
}
