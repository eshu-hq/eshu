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
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SupplyChainImpactTx is the transaction surface one impact write runs in.
// The conflict-domain lock, the finding upsert, and the retraction of
// superseded findings must commit or roll back together (#6831).
type SupplyChainImpactTx interface {
	factwrite.Execer
	// QueryContext runs a read inside the write transaction: the changed-row
	// read and the recovery CVE read (#9695).
	QueryContext(ctx context.Context, query string, args ...any) (SupplyChainImpactRows, error)
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
	meta []changedRow,
	protected ProtectedScanImpactPublicationSet,
	hasProtected bool,
	now time.Time,
) (supplyChainImpactTxOutcome, error) {
	tx, err := beginner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return supplyChainImpactTxOutcome{}, fmt.Errorf("begin supply chain impact transaction: %w", err)
	}
	outcome, err := commitSupplyChainImpactRows(ctx, tx, write, rows, meta, protected, hasProtected, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("%w; rollback: %v", err, rbErr)
		}
		return supplyChainImpactTxOutcome{}, err
	}
	if err := tx.Commit(); err != nil {
		return supplyChainImpactTxOutcome{}, fmt.Errorf("commit supply chain impact transaction: %w", err)
	}
	return outcome, nil
}

// supplyChainImpactTxOutcome is what one committed pass reports: how many rows
// it retracted, which CVEs its rows changed, and, for a recovery write, every
// CVE the scope's rows name. suppressionCVEs are the CVEs the pass's changed
// suppressions name (#10019); suppression is the diff report behind them.
type supplyChainImpactTxOutcome struct {
	retracted       int
	changedCVEs     []string
	derivedCVEs     []string
	recoveryCVEs    []string
	suppressionCVEs []string
	// touchedGroups are the coalesced advisory-group ids (#10411) the
	// pass's changed rows, changed suppressions, and (on recovery) every
	// stored row name. See SupplyChainImpactWriteResult.TouchedGroupIDs.
	touchedGroups []string
	suppression   suppressionDiffReport
	// passStateSkipped reports the generation fence held (#10422): the
	// pass's generation was stale, so the per-scope pass-state row was
	// left untouched.
	passStateSkipped bool
}

// commitSupplyChainImpactRows runs one pass's statements inside tx and returns
// what it retracted and changed.
func commitSupplyChainImpactRows(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	rows []factwrite.VersionedRow,
	meta []changedRow,
	protected ProtectedScanImpactPublicationSet,
	hasProtected bool,
	now time.Time,
) (supplyChainImpactTxOutcome, error) {
	var publisher ProtectedScanImpactPublisher
	var authorizer ProtectedScanImpactAuthorizer
	if hasProtected {
		var ok bool
		publisher, ok = tx.(ProtectedScanImpactPublisher)
		if !ok {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("protected scan impact publication transaction is required")
		}
		authorizer, ok = tx.(ProtectedScanImpactAuthorizer)
		if !ok {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("protected scan impact authorization transaction is required")
		}
		if err := authorizer.AuthorizeProtectedScanImpactResults(ctx, protected); err != nil {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("authorize protected scan impact result: %w", err)
		}
	}
	// Lock before any impact row lock this transaction takes, so the only
	// wait two passes of one (scope, generation) can form is on this lock:
	// no lock-order cycle is possible. Passes for other scopes or
	// generations hash to other keys and never wait here.
	if _, err := tx.ExecContext(
		ctx, lockSupplyChainImpactConflictDomainQuery,
		supplyChainImpactConflictDomainKey(write.ScopeID, write.GenerationID),
	); err != nil {
		return supplyChainImpactTxOutcome{}, fmt.Errorf("lock supply chain impact conflict domain: %w", err)
	}
	// The suppression diff runs before the admission row is written: its
	// duplicate answer reads only previously committed admissions, so a
	// first pass is never judged by its own upsert, and the fail-closed
	// answer for a pending generation rolls back with no admission row. The
	// union lands on changedCVEs below, before the owed marker is recorded,
	// so a suppression-only change still sets the marker (#10019).
	supp, err := runSuppressionDiff(ctx, tx, write.ScopeID, write.GenerationID,
		write.ScopeFactKinds, write.Recovery, write.SuppressionDiffTimeout)
	if err != nil {
		return supplyChainImpactTxOutcome{suppression: supp}, err
	}
	// Admission second, still before any row is touched: a pass whose fencing
	// token is older than one already admitted for this (scope, generation) is
	// rejected whole. Without it a stale pass that commits after a fresher one
	// still inserts fact ids the fresher pass did not derive, leaving a union
	// no pass derived (#7142). A partial-evidence pass is admitted like any
	// other: it upserts, and only the retraction below is skipped.
	admitted, err := tryAdmitSupplyChainImpactWrite(ctx, tx, write.ScopeID, write.GenerationID, write.FencingToken, now)
	if err != nil {
		return supplyChainImpactTxOutcome{}, err
	}
	if !admitted {
		return supplyChainImpactTxOutcome{}, supplyChainImpactWriteSupersededError{
			scopeID:      write.ScopeID,
			generationID: write.GenerationID,
			fencingToken: write.FencingToken,
		}
	}
	// Read what the pass is about to overwrite, after the lock (no concurrent
	// pass of this conflict domain can change it) and before the upsert, so the
	// change set compares the new rows with the rows they replace (#9695).
	stored, err := readStoredSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID)
	if err != nil {
		return supplyChainImpactTxOutcome{suppression: supp}, err
	}
	// Bounded chunked bulk insert: findings are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per finding.
	if err := factwrite.BatchInsertVersionedFacts(ctx, tx, rows); err != nil {
		return supplyChainImpactTxOutcome{}, fmt.Errorf("write supply chain impact fact: %w", err)
	}
	// Name the keys this pass changed for the winners keyed resweep (#9805), in
	// the transaction that changes them. A rewrite to an identical digest names
	// nothing, so an unchanged rerun issues no extra statement.
	if capturer, ok := tx.(SupplyChainImpactWinnersCapturer); ok {
		if idents := dirtyIdentifiersToCapture(meta, stored, write.GenerationID); len(idents) > 0 {
			if _, err := capturer.CaptureWinnersDirtyFacts(ctx, idents); err != nil {
				return supplyChainImpactTxOutcome{}, fmt.Errorf("capture supply chain impact winners dirty keys: %w", err)
			}
		}
	}
	if hasProtected {
		if err := publisher.PublishProtectedScanImpactResults(ctx, protected); err != nil {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("publish protected scan impact result: %w", err)
		}
	}
	rowChanged := changedCVEIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence)
	if len(supp.cveIDs) > 0 {
		rowChanged = unionSortedStrings(rowChanged, supp.cveIDs)
	}
	// The touched-group set (#10411) is computed from the same meta and
	// stored rows with the same change judgement, keyed coalesced so
	// CVE-less rows name their advisory group. Suppression ids join it the
	// way they join changedCVEs: a changed suppression names CVEs, and a
	// group keyed by CVE is exactly that id. A suppression that scopes no
	// CVE names no group here either — the same blind spot
	// countSuppressionsWithoutCVEScope reports for the holder fan-out.
	touchedGroups := touchedGroupIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence)
	if len(supp.cveIDs) > 0 {
		touchedGroups = unionSortedStrings(touchedGroups, supp.cveIDs)
	}
	// Derived is the disjoint-compare input (#9740): every row the pass
	// wrote plus every row its retraction tombstones. The suppression diff
	// joins the union because a suppression-only change rewrites rows the
	// meta below already carries; naming its ids twice is harmless.
	derived := derivedCVEIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence, write.ScopedCVEIDs)
	derived = unionSortedStrings(derived, supp.cveIDs)
	outcome := supplyChainImpactTxOutcome{
		changedCVEs:     rowChanged,
		derivedCVEs:     derived,
		suppressionCVEs: supp.cveIDs,
		touchedGroups:   touchedGroups,
		suppression:     supp,
	}
	if write.Owed != nil && len(outcome.changedCVEs) > 0 {
		if err := write.Owed.RecordFanOutOwed(ctx, tx); err != nil {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("record supply chain impact fan-out owed: %w", err)
		}
	}
	if !write.PartialEvidence {
		keep := make([]string, 0, len(rows))
		for _, row := range rows {
			keep = append(keep, row.FactID)
		}
		outcome.retracted, err = retractSupersededSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID, keep, write.FencingToken, write.ScopedCVEIDs)
		if err != nil {
			return supplyChainImpactTxOutcome{}, err
		}
		// The retract statement appended one dirty-log row per tombstoned
		// finding, so its affected-row count is the capture count.
		if capturer, ok := tx.(SupplyChainImpactWinnersCapturer); ok {
			capturer.RecordWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceFactRetract, int64(outcome.retracted))
		}
	}
	if write.Recovery {
		outcome.recoveryCVEs, err = readSupplyChainImpactCVEIDs(ctx, tx, write.ScopeID, write.GenerationID)
		if err != nil {
			return supplyChainImpactTxOutcome{}, err
		}
		recoveryGroups, err := readSupplyChainImpactGroupIDs(ctx, tx, write.ScopeID, write.GenerationID)
		if err != nil {
			return supplyChainImpactTxOutcome{}, err
		}
		outcome.touchedGroups = unionSortedStrings(outcome.touchedGroups, recoveryGroups)
	}
	// Record the scope's durable per-scope pass state last (#10389): a
	// partial pass marks the scope stuck with its causes, a full complete
	// pass clears it, and a scoped complete leaves the state untouched
	// because it re-derives only its stamped subset (#9779). It runs on the
	// admitted path only, and it fails the pass when the state cannot be
	// recorded: state that disagrees with the committed rows is worse than
	// a retried pass. A stale generation's pass records nothing (#10422);
	// the skip rides the outcome to a handler signal so operators can see
	// it. Late placement is load-bearing: the fence decides on the
	// freshest READ COMMITTED snapshot, so a newer generation that commits
	// while this pass's bulk writes run is seen and the stale write
	// skipped, instead of committing over the newer clear. Only a commit
	// landing between this statement and the commit below can still
	// interleave, and no bulk work runs in that window.
	outcome.passStateSkipped, err = recordSupplyChainImpactScopePassState(ctx, tx, write, now)
	if err != nil {
		return supplyChainImpactTxOutcome{}, err
	}
	return outcome, nil
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
//
// The tombstoned rows' keys are appended to the winners dirty log by the same
// statement (#9805): one log row per retracted finding, so the affected-row
// count of the INSERT is the retracted count and no Go change is needed to name
// a retracted finding's key. A retracted row is always a change.
const retractSupersededSupplyChainImpactFindingsQuery = `
WITH retracted AS (
    UPDATE fact_records
    SET is_tombstone = TRUE,
        fencing_token = $5
    WHERE fact_kind = $1
      AND scope_id = $2
      AND generation_id = $3
      AND is_tombstone = FALSE
      AND fact_id NOT IN (SELECT unnest($4::text[]))
      AND fencing_token <= $5
    RETURNING
        COALESCE(payload->>'package_id', '') AS package_id,
        COALESCE(payload->>'repository_id', '') AS repository_id,
        COALESCE(payload->>'subject_digest', '') AS subject_digest,
        COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '') AS c0,
        COALESCE(payload->>'advisory_id', '') AS advisory_id
)
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_retract', package_id, repository_id, subject_digest, c0, advisory_id
FROM retracted
`

// retractSupersededSupplyChainImpactFindingsScopedQuery is the scoped-pass
// variant of the replace-set retraction (#9779). The keep set still names the
// rows the pass just wrote, but the statement only tombstones rows inside the
// stamped CVE set: rows outside it are not the pass's to retire, so they are
// left byte-identical however stale the next full pass would find them. Rows
// without a cve_id never match the set and converge on the next full pass.
const retractSupersededSupplyChainImpactFindingsScopedQuery = `
WITH retracted AS (
    UPDATE fact_records
    SET is_tombstone = TRUE,
        fencing_token = $5
    WHERE fact_kind = $1
      AND scope_id = $2
      AND generation_id = $3
      AND is_tombstone = FALSE
      AND fact_id NOT IN (SELECT unnest($4::text[]))
      AND fencing_token <= $5
      AND payload->>'cve_id' = ANY($6::text[])
    RETURNING
        COALESCE(payload->>'package_id', '') AS package_id,
        COALESCE(payload->>'repository_id', '') AS repository_id,
        COALESCE(payload->>'subject_digest', '') AS subject_digest,
        COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '') AS c0,
        COALESCE(payload->>'advisory_id', '') AS advisory_id
)
INSERT INTO supply_chain_impact_winners_dirty_log
    (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT 'fact', 'fact_retract', package_id, repository_id, subject_digest, c0, advisory_id
FROM retracted
`

// retractSupersededSupplyChainImpactFindings runs the replace-set retraction
// for one pass and returns how many rows it tombstoned. A scoped pass
// (#9779) retracts inside its stamped CVE set only.
func retractSupersededSupplyChainImpactFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
	keepFactIDs []string,
	fencingToken int64,
	scopedCVEIDs []string,
) (int, error) {
	query := retractSupersededSupplyChainImpactFindingsQuery
	args := []any{
		supplyChainImpactFactKind,
		scopeID,
		generationID,
		keepFactIDs,
		fencingToken,
	}
	if len(scopedCVEIDs) > 0 {
		query = retractSupersededSupplyChainImpactFindingsScopedQuery
		args = append(args, scopedCVEIDs)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("retract superseded supply chain impact findings: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read supply chain impact retraction rows affected: %w", err)
	}
	return int(affected), nil
}
