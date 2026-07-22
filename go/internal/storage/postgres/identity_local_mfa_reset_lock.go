// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
)

const (
	// localIdentityMFAResetAdvisoryLockPrefix namespaces the per-user MFA
	// reset advisory lock key. Go-computed FNV-64a keys and small fixed
	// integer keys (3455, 3456 — see identity_local_sql.go and
	// identity_bootstrap_credential_sql.go) share the same
	// pg_advisory_xact_lock(bigint) keyspace; the prefix keeps this domain's
	// keys collision-free from any other named lock, mirroring the same
	// per-entity pattern platform_graph_locker.go and
	// package_registry_identity_locker.go already use.
	localIdentityMFAResetAdvisoryLockPrefix = "eshu:local_identity_mfa_reset:"
	localIdentityMFAResetAdvisoryLockQuery  = "SELECT pg_advisory_xact_lock($1::bigint)"
	maxLocalIdentityMFAResetAdvisoryLockKey = uint64(1<<63 - 1)
)

// lockLocalIdentityMFAReset acquires a transaction-scoped, per-user advisory
// lock before ResetLocalIdentityMFA's revoke/insert statements run, so two
// concurrent resets for the SAME user_id serialize instead of both
// committing an active identity_mfa_factors row.
// identity_mfa_factors_user_active_idx (identity_subjects.go) is a
// non-unique partial index — there is no unique constraint enforcing "at
// most one active factor per (user_id, factor_kind)" — so without this lock,
// two concurrent resets can each run their revoke UPDATE against zero
// matching rows (nothing pre-existing to lock) and then both INSERT an
// unconditionally successful new active row, leaving two simultaneously
// active recovery-code factors for one user. Postgres releases the lock
// automatically on commit or rollback.
//
// Lock-ordering invariant: this is the ONLY advisory lock
// ResetLocalIdentityMFA ever takes. It never takes 3455
// (localIdentityBootstrapLockQuery, identity_local_sql.go) or 3456
// (bootstrapCredentialLockQuery, identity_bootstrap_credential_sql.go). Every
// OTHER caller that also mutates identity_mfa_factors /
// identity_mfa_recovery_codes for a user inside a 3456-guarded transaction
// acquires this same per-user key derived from
// localIdentityMFAResetAdvisoryLockKey AFTER 3456, mirroring
// GenerateBootstrapAdminWithCredential's fixed 3455-then-3456 ordering
// (identity_bootstrap_credential.go). The two such callers are
// ResetBootstrapCredential (its reenrollBootstrapCredentialRecoveryFactor
// step, identity_bootstrap_credential.go) and CompleteSetupMFA
// (identity_setup_completion.go); both take 3456 first, then this key.
// Because ResetLocalIdentityMFA never takes 3456, and those callers always
// take 3456 before this key, no wait-for cycle can form: the lock hierarchy
// is a strict linear 3455 -> 3456 -> per-user chain, so this per-user key is
// always the innermost (last-acquired, first-released) lock in any
// transaction that holds it, never held by a transaction that is also
// waiting on 3456.
func lockLocalIdentityMFAReset(ctx context.Context, tx Transaction, userID string) error {
	if _, err := tx.ExecContext(
		ctx,
		localIdentityMFAResetAdvisoryLockQuery,
		localIdentityMFAResetAdvisoryLockKey(userID),
	); err != nil {
		return fmt.Errorf("acquire local identity mfa reset lock: %w", err)
	}
	return nil
}

// localIdentityMFAResetAdvisoryLockKey deterministically derives the
// per-user pg_advisory_xact_lock key from the namespaced prefix and user ID,
// so every caller locking the same user converges on the same key.
func localIdentityMFAResetAdvisoryLockKey(userID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(localIdentityMFAResetAdvisoryLockPrefix))
	_, _ = h.Write([]byte(strings.TrimSpace(userID)))
	return int64(h.Sum64() & maxLocalIdentityMFAResetAdvisoryLockKey)
}
