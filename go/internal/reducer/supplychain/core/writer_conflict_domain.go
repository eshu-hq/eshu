// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// lockSupplyChainImpactConflictDomainQuery serializes the write transactions
// of one (scope, generation) finding set (#6831). Replace-set semantics need
// it: under Read Committed a pass's retraction cannot see rows a concurrent,
// still-uncommitted pass just upserted, so two overlapping passes would each
// keep their own rows and commit the union of two different finding sets --
// truth neither pass derived. With the lock, the later pass's retraction runs
// after the earlier pass commits and sees its rows, so the committed set is
// exactly the last pass's. The lock is transaction-scoped and released at
// commit or rollback; only the write transaction serializes, never evidence
// loading or finding construction, and passes for other (scope, generation)
// pairs proceed concurrently.
const lockSupplyChainImpactConflictDomainQuery = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// supplyChainImpactConflictDomainKey is the advisory-lock key text for one
// (scope, generation) finding set. The fact kind prefix namespaces it away
// from other domains' advisory keys; a 64-bit hash collision only costs a
// brief extra wait, never a wrong result.
//
// The go-evidence lock key (SupplyChainImpactGoEvidenceLockKey, go_evidence_lock.go)
// is a separate derivation in a separate namespace, taken after this lock, at the end of the transaction.
func supplyChainImpactConflictDomainKey(scopeID, generationID string) string {
	return supplyChainImpactFactKind + "\x1f" + scopeID + "\x1f" + generationID
}

// SupplyChainImpactConflictDomainLockQuery is the transaction-scoped advisory
// lock statement the impact writer takes for one (scope, generation). The
// per-support backfill takes the same lock with the same key, so it cannot
// interleave with a writer's fact and support writes or its retraction.
const SupplyChainImpactConflictDomainLockQuery = lockSupplyChainImpactConflictDomainQuery

// SupplyChainImpactConflictDomainKey returns the lock key text of one (scope,
// generation) finding set, the argument of SupplyChainImpactConflictDomainLockQuery.
func SupplyChainImpactConflictDomainKey(scopeID, generationID string) string {
	return supplyChainImpactConflictDomainKey(scopeID, generationID)
}
