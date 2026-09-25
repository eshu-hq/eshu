// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
) (int, error) {
	tx, err := beginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin supply chain impact transaction: %w", err)
	}
	retracted, err := commitSupplyChainImpactRows(ctx, tx, write, rows)
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
	return retractSupersededSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID, keep, 0)
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
// pass carrying a lower one.
const retractSupersededSupplyChainImpactFindingsQuery = `
UPDATE fact_records
SET is_tombstone = TRUE
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
