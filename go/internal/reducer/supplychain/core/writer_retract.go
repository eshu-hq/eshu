// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// SupplyChainImpactTx is the transaction surface one impact write runs in.
// The conflict-domain lock, the finding upsert, and the retraction of
// superseded findings must commit or roll back together (#6831).
type SupplyChainImpactTx interface {
	factwrite.Execer
	Commit() error
	Rollback() error
}

// SupplyChainImpactBeginner opens the transaction for one impact write.
// Production wires postgres.SupplyChainImpactBeginner over the reducer's
// instrumented database.
type SupplyChainImpactBeginner interface {
	BeginSupplyChainImpactTx(context.Context) (SupplyChainImpactTx, error)
}

// runSupplyChainImpactTx runs one pass in its own transaction: commit on
// success, rollback on any statement failure, so a pass never leaves a
// half-applied upsert or retraction behind.
func runSupplyChainImpactTx(
	ctx context.Context,
	beginner SupplyChainImpactBeginner,
	write SupplyChainImpactWrite,
	rows []factwrite.VersionedRow,
	now time.Time,
) (int, error) {
	tx, err := beginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin supply chain impact transaction: %w", err)
	}
	retracted, err := commitSupplyChainImpactRows(ctx, tx, write, rows, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return 0, fmt.Errorf("%w; rollback: %v", err, rbErr)
		}
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit supply chain impact transaction: %w", err)
	}
	return retracted, nil
}

// commitSupplyChainImpactRows runs one pass's statements inside tx and returns
// how many superseded finding rows it retracted.
func commitSupplyChainImpactRows(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	rows []factwrite.VersionedRow,
	now time.Time,
) (int, error) {
	// Lock first, before any row lock this transaction takes, so the only
	// wait two passes of one (scope, generation) can form is on this lock:
	// no lock-order cycle is possible. Passes for other scopes or
	// generations hash to other keys and never wait here.
	if _, err := tx.ExecContext(
		ctx, lockSupplyChainImpactConflictDomainQuery,
		supplyChainImpactConflictDomainKey(write.ScopeID, write.GenerationID),
	); err != nil {
		return 0, fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	// Admission second, still before any row is touched: a pass whose fencing
	// token is older than one already admitted for this (scope, generation) is
	// rejected whole. Without it a stale pass that commits after a fresher one
	// still inserts fact ids the fresher pass did not derive, leaving a union
	// no pass derived (#7142). A partial-evidence pass is admitted like any
	// other: it upserts, and only the retraction below is skipped.
	admitted, err := tryAdmitSupplyChainImpactWrite(ctx, tx, write.ScopeID, write.GenerationID, write.FencingToken, now)
	if err != nil {
		return 0, err
	}
	if !admitted {
		return 0, supplyChainImpactWriteSupersededError{
			scopeID:      write.ScopeID,
			generationID: write.GenerationID,
			fencingToken: write.FencingToken,
		}
	}
	// Bounded chunked bulk insert: findings are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per finding.
	if err := factwrite.BatchInsertVersionedFacts(ctx, tx, rows); err != nil {
		return 0, fmt.Errorf("write supply chain impact fact: %w", err)
	}
	if write.PartialEvidence {
		return 0, nil
	}
	keep := make([]string, 0, len(rows))
	for _, row := range rows {
		keep = append(keep, row.FactID)
	}
	return retractSupersededSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID, keep, write.FencingToken)
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
func supplyChainImpactConflictDomainKey(scopeID, generationID string) string {
	return supplyChainImpactFactKind + "\x1f" + scopeID + "\x1f" + generationID
}

// retractSupersededSupplyChainImpactFindingsQuery tombstones every active
// finding row of this (scope, generation) that the current pass did not just
// write (#6831). A finding's fact id embeds its whole logical identity,
// including repository_id, so a pass that later anchors a finding to a
// repository mints a NEW fact id; without this statement the earlier repo-less
// row stays active forever next to it.
//
// Rows are tombstoned, never deleted: the upsert's is_tombstone reset revives
// a row a later pass derives again, and the tombstone stays auditable.
//
// NOT IN (SELECT unnest($4)) instead of fact_id <> ALL($4): the reducer's pgx
// connection caches prepared statements, and under a generic plan
// <> ALL($param) is a linear per-row array scan -- measured 2889 ms for 20000
// scope rows against a 10000-id keep set, versus 57 ms for the hashed subplan
// this form always gets (docs/internal/evidence/6831-supply-chain-impact-replace-set.md).
// The keep set never carries NULL, so NOT IN has no NULL trap here.
//
// fencing_token <= $5 applies the insert's conflict guard to the retraction:
// a row a fresher writer stamped with a higher token is never retracted by a
// pass carrying a lower one. The tombstone is stamped with the retiring pass's
// token (#7142), so a tombstone records which pass retired the row and the
// upsert guard (existing <= EXCLUDED) refuses a stale pass's revive of it.
// Legacy rows written before the token existed carry 0 and are retracted by any
// pass (0 <= $5).
const retractSupersededSupplyChainImpactFindingsQuery = `
UPDATE fact_records
SET is_tombstone = TRUE,
    fencing_token = $5
WHERE fact_kind = $1
  AND scope_id = $2
  AND generation_id = $3
  AND is_tombstone = FALSE
  AND fact_id NOT IN (SELECT unnest($4::text[]))
  AND fencing_token <= $5
`

// retractSupersededSupplyChainImpactFindings runs the replace-set retraction
// for one pass and returns how many rows it tombstoned.
func retractSupersededSupplyChainImpactFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
	keepFactIDs []string,
	fencingToken int64,
) (int, error) {
	result, err := tx.ExecContext(
		ctx,
		retractSupersededSupplyChainImpactFindingsQuery,
		supplyChainImpactFactKind,
		scopeID,
		generationID,
		keepFactIDs,
		fencingToken,
	)
	if err != nil {
		return 0, fmt.Errorf("retract superseded supply chain impact findings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read supply chain impact retraction rows affected: %w", err)
	}
	return int(affected), nil
}

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
