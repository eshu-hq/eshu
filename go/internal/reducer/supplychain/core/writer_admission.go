// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SupplyChainImpactWriteSupersededFailureClass classifies a write whose
// evidence-read fencing token is older than one this (scope, generation) pair
// already admitted (#7142). The reducer queue treats it as a non-counting
// retry class (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): losing this
// race is not the item's fault, and a retry re-reads evidence with a fresher
// token and, absent a still-fresher concurrent pass, is admitted.
const SupplyChainImpactWriteSupersededFailureClass = "supply_chain_impact_write_superseded"

// SupplyChainImpactFencingTokenIssuer supplies the database-issued,
// monotonically increasing fencing token each impact pass is stamped with
// (#7142). Backed by a Postgres sequence
// (supply_chain_impact_fencing_token_seq, migration 154) in production, so the
// value reflects real cross-replica issuance order rather than any reducer
// host's wall clock.
//
// SupplyChainImpactHandler.Handle calls it once per invocation, immediately
// BEFORE the evidence load, never at write-commit time. The token orders passes
// by the order they began reading evidence. A commit-time token would order them
// by commit order instead, so a worker that read stale evidence early but
// committed last would out-rank the fresher worker that committed first: the
// stale-retraction hazard the token exists to close. What the token does NOT
// order is passes whose loads interleave: the load is many statements with no
// snapshot, so a pass that took its token first but read later can carry the
// lower token while holding the fresher evidence. That residual is no worse than
// before the fence (last committer won) and converges only when a later intent
// for the same (scope, generation) runs. Every Handle call, including a retry or
// a redelivery after reclaim, draws a fresh token; never reuse one per work item.
// The same reasoning and the wall-clock failure it replaced are documented on the
// AWS cloud runtime drift issuer (aws_cloud_runtime_drift_admission.go).
type SupplyChainImpactFencingTokenIssuer interface {
	// NextSupplyChainImpactFencingToken returns the next value in issuance
	// order. It never returns the same value twice and never returns 0.
	NextSupplyChainImpactFencingToken(ctx context.Context) (int64, error)
}

// supplyChainImpactAdmissionQuery is the begin-before-mutate admission check.
// It records the highest fencing token any pass has been admitted to write
// with for one (scope, generation) and applies only when this pass's token is
// at least the stored one: RowsAffected() == 0 means a fresher pass already
// admitted, so this pass must not upsert or retract anything.
//
// A watermark row, rather than MAX(fencing_token) over fact_records, because a
// fresher pass that derived an EMPTY finding set leaves no row to take a
// maximum over, and a stale pass would then be admitted and publish findings
// the fresher evidence says do not exist. Every admitted pass writes the
// watermark, including empty and partial ones.
//
// `<=`, not `<`: an IDENTICAL write executed twice (the same write value, hence
// the same token) is admitted, so the writer stays idempotent at its own
// boundary. Handle draws a fresh token per invocation, so a retry or a
// redelivery never carries an equal token; an equal token only arises when a
// caller re-executes one write value. A sequence never issues one value to two
// passes, so an equal token is never a different pass.
const supplyChainImpactAdmissionQuery = `
INSERT INTO supply_chain_impact_write_admission (
    scope_id, generation_id, fencing_token, updated_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (scope_id, generation_id) DO UPDATE SET
    fencing_token = EXCLUDED.fencing_token,
    updated_at    = EXCLUDED.updated_at
WHERE supply_chain_impact_write_admission.fencing_token <= EXCLUDED.fencing_token
`

// supplyChainImpactWriteSupersededError marks an admission rejection as
// retryable so the durable queue re-runs the intent instead of a stalled
// pass's write landing after a fresher pass already committed. It is an error,
// not a success with zero writes: a zombie worker's Fail() is lease-fenced, so
// it cannot disturb a reclaimed item, and dead-lettering would freeze stale
// truth.
type supplyChainImpactWriteSupersededError struct {
	scopeID      string
	generationID string
	fencingToken int64
}

func (e supplyChainImpactWriteSupersededError) Error() string {
	return fmt.Sprintf(
		"supply chain impact write superseded: a fresher pass already admitted for scope %s generation %s (fencing token %d)",
		e.scopeID, e.generationID, e.fencingToken,
	)
}

func (supplyChainImpactWriteSupersededError) Retryable() bool { return true }

func (supplyChainImpactWriteSupersededError) FailureClass() string {
	return SupplyChainImpactWriteSupersededFailureClass
}

// errSupplyChainImpactMissingFencingToken is returned when a write reaches the
// writer with a zero FencingToken: a caller that forgot to wire
// SupplyChainImpactFencingTokenIssuer, or bypassed Handle. A Postgres sequence
// never issues 0, so zero means "never issued". Deliberately a hard error
// rather than a default: a defaulted token would make the upsert guard and the
// retraction predicate inert again.
var errSupplyChainImpactMissingFencingToken = errors.New(
	"supply chain impact write requires a non-zero fencing_token: the admission check and the durable rows have no ordering value to be stamped with",
)

// tryAdmitSupplyChainImpactWrite runs the admission check inside the write
// transaction and reports whether this pass may proceed. It runs after the
// conflict-domain advisory lock and before any upsert or retraction.
func tryAdmitSupplyChainImpactWrite(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
	fencingToken int64,
	now time.Time,
) (bool, error) {
	result, err := tx.ExecContext(ctx, supplyChainImpactAdmissionQuery, scopeID, generationID, fencingToken, now)
	if err != nil {
		return false, fmt.Errorf("supply chain impact write admission: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("supply chain impact write admission rows affected: %w", err)
	}
	return affected > 0, nil
}

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
