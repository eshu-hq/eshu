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
		// Only the lock-wait observation and the statement timings accumulated
		// before the failure survive a failed pass: they are durations and
		// counts, not results. The caller records the timings under
		// outcome=rolled_back, beside RecordSupplyChainImpactWriteTx.
		failed := supplyChainImpactTxOutcome{goEvidenceLocked: outcome.goEvidenceLocked, goEvidenceLockWait: outcome.goEvidenceLockWait, statements: outcome.statements}
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return failed, fmt.Errorf("%w; rollback: %v", err, rbErr)
		}
		return failed, err
	}
	commitStarted := time.Now()
	commitErr := tx.Commit()
	outcome.statements.add(supplyChainImpactTxStmtCommit, time.Since(commitStarted), 0)
	if commitErr != nil {
		// The lock-wait observation and the statement timings survive a failed
		// commit too: the pass ran its statements and timed its commit.
		failed := supplyChainImpactTxOutcome{goEvidenceLocked: outcome.goEvidenceLocked, goEvidenceLockWait: outcome.goEvidenceLockWait, statements: outcome.statements}
		return failed, fmt.Errorf("commit supply chain impact transaction: %w", commitErr)
	}
	return outcome, nil
}

// commitSupplyChainImpactRowsTimed is the timed body behind
// commitSupplyChainImpactRows: the same statements in the same order, each
// region folded into timings with clock reads and struct stores only, never
// logging or metric IO.
func commitSupplyChainImpactRowsTimed(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	rows []factwrite.VersionedRow,
	meta []changedRow,
	protected ProtectedScanImpactPublicationSet,
	hasProtected bool,
	now time.Time,
	timings *supplyChainImpactTxStatementTimings,
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
	lockStarted := time.Now()
	_, lockErr := tx.ExecContext(
		ctx, lockSupplyChainImpactConflictDomainQuery,
		supplyChainImpactConflictDomainKey(write.ScopeID, write.GenerationID),
	)
	timings.add(supplyChainImpactTxStmtConflictLock, time.Since(lockStarted), 0)
	if lockErr != nil {
		return supplyChainImpactTxOutcome{}, fmt.Errorf("lock supply chain impact conflict domain: %w", lockErr)
	}
	// The suppression diff runs before the admission row is written: its
	// duplicate answer reads only previously committed admissions, so a
	// first pass is never judged by its own upsert, and the fail-closed
	// answer for a pending generation rolls back with no admission row. The
	// union lands on changedCVEs below, before the owed marker is recorded,
	// so a suppression-only change still sets the marker (#10019).
	suppStarted := time.Now()
	supp, err := runSuppressionDiff(ctx, tx, write.ScopeID, write.GenerationID,
		write.ScopeFactKinds, write.Recovery, write.SuppressionDiffTimeout)
	timings.add(supplyChainImpactTxStmtSuppressionDiff, time.Since(suppStarted), 0)
	if err != nil {
		return supplyChainImpactTxOutcome{suppression: supp}, err
	}
	// Admission second, still before any row is touched: a pass whose fencing
	// token is older than one already admitted for this (scope, generation) is
	// rejected whole. Without it a stale pass that commits after a fresher one
	// still inserts fact ids the fresher pass did not derive, leaving a union
	// no pass derived (#7142). A partial-evidence pass is admitted like any
	// other: it upserts, and only the retraction below is skipped.
	admissionStarted := time.Now()
	admitted, err := tryAdmitSupplyChainImpactWrite(ctx, tx, write.ScopeID, write.GenerationID, write.FencingToken, now)
	timings.add(supplyChainImpactTxStmtAdmission, time.Since(admissionStarted), 0)
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
	storedStarted := time.Now()
	stored, err := readStoredSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID)
	timings.add(supplyChainImpactTxStmtStoredRead, time.Since(storedStarted), int64(len(stored)))
	if err != nil {
		return supplyChainImpactTxOutcome{suppression: supp}, err
	}
	// The fact rows and the supports derived from them, in this transaction and
	// under the lock above (WriteSupplyChainImpactFindingRows is the one step
	// every admission of finding rows runs, the advisory-group cell's included).
	if err := WriteSupplyChainImpactFindingRows(ctx, tx, rows, timings); err != nil {
		return supplyChainImpactTxOutcome{}, err
	}
	supports, hasSupports := tx.(SupplyChainImpactSupportsMaintainer)
	// keep is the fact-id set this pass wrote: the retraction below tombstones
	// every other active row of the generation. It is built once so the
	// capture and the retraction name the same set.
	keep := make([]string, 0, len(rows))
	for _, row := range rows {
		keep = append(keep, row.FactID)
	}
	// Name the keys this pass changed for the winners keyed resweep (#9805), in
	// the transaction that changes them. A rewrite to an identical digest names
	// nothing, so an unchanged rerun issues no extra statement. A key this
	// pass's own retraction tombstones is named there, so the capture leaves
	// it out: one pass stages one dirty row per key (#11411).
	if err := captureTimedWinnersDirtyKeys(ctx, tx, write, meta, stored, keep, timings); err != nil {
		return supplyChainImpactTxOutcome{}, err
	}
	if hasProtected {
		if err := publisher.PublishProtectedScanImpactResults(ctx, protected); err != nil {
			return supplyChainImpactTxOutcome{}, fmt.Errorf("publish protected scan impact result: %w", err)
		}
	}
	rowChanged := changedCVEIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence)
	suppMembers := expandSuppressionCVEIDsThroughMembers(supp.cveIDs, meta, stored)
	if len(suppMembers) > 0 {
		rowChanged = unionSortedStrings(rowChanged, suppMembers)
	}
	// The touched-group set (#10411) is computed from the same meta and
	// stored rows with the same change judgement, keyed coalesced so
	// CVE-less rows name their advisory group. Suppression ids join it the
	// way they join changedCVEs: a changed suppression names CVE and
	// advisory ids (#10409), and a group keyed by either id is named by
	// exactly that id. A suppression that scopes neither id names no group
	// here either — the same blind spot
	// countSuppressionsWithoutIdentityScope reports for the holder fan-out.
	touchedGroups := touchedGroupIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence)
	if len(supp.cveIDs) > 0 {
		touchedGroups = unionSortedStrings(touchedGroups, supp.cveIDs)
	}
	// Derived is the disjoint-compare input (#9740): every row the pass
	// wrote plus every row its retraction tombstones. The suppression diff
	// joins the union because a suppression-only change rewrites rows the
	// meta below already carries; naming its ids twice is harmless.
	derived := unionSortedStrings(derivedCVEIDs(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence, write.ScopedCVEIDs), suppMembers)
	idMeta, idStored := identityChangedRows(meta, stored)
	idChanged := unionSortedStrings(changedCVEIDs(idMeta, idStored, write.GenerationID, write.FencingToken, !write.PartialEvidence), suppMembers)
	idTouched := unionSortedStrings(touchedGroupIDs(idMeta, idStored, write.GenerationID, write.FencingToken, !write.PartialEvidence), supp.cveIDs)
	outcome := supplyChainImpactTxOutcome{
		identityChangedCVEs:   idChanged,
		identityTouchedGroups: idTouched,
		changedCVEs:           rowChanged,
		derivedCVEs:           derived,
		suppressionCVEs:       supp.cveIDs,
		touchedGroups:         touchedGroups,
		suppression:           supp,
	}
	// The genuine identity-change subset rides alongside the touched set so
	// the D6 counter fires only on real re-keys, never on commit shape
	// (#10408 F1): ordinary EPSS-only refreshes, admissions, rollovers, and
	// same-key rewrites report Count false.
	outcome.rekey = reportRekeyIdentity(meta, stored, write.GenerationID, write.FencingToken, !write.PartialEvidence)
	outcome.staleCopyRepairs = staleCopyRepairsOf(meta, stored, write.GenerationID)
	// The owed marker also covers the advisory-group enqueue (#10411): a
	// pass whose only changes are CVE-less (group keys live in advisory_id)
	// has empty changedCVEs but still owes its touched groups a
	// projection, so the crash-recovery rerun must re-drive it too.
	// The marker updates this pass's own fact_work_items row, the row a fence's holder
	// reopen updates, so it is recorded after the go-evidence lock below: holding it while
	// waiting for the lock would close a cycle with a fence holding the exclusive lock.
	owed := write.Owed != nil && (len(outcome.changedCVEs) > 0 || len(outcome.touchedGroups) > 0)
	if !write.PartialEvidence {
		tombstoned, err := retractTimedSupersededFindings(ctx, tx, write, keep, supports, hasSupports, timings)
		if err != nil {
			return supplyChainImpactTxOutcome{}, err
		}
		outcome.retracted = tombstoned
	}
	if write.Recovery {
		recoveryCVEs, recoveryGroups, err := readTimedRecoveryReport(ctx, tx, write, timings)
		if err != nil {
			return supplyChainImpactTxOutcome{}, err
		}
		outcome.recoveryCVEs = recoveryCVEs
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
	passStateStarted := time.Now()
	outcome.passStateSkipped, err = recordSupplyChainImpactScopePassState(ctx, tx, write, now)
	timings.add(supplyChainImpactTxStmtPassState, time.Since(passStateStarted), 0)
	if err != nil {
		return supplyChainImpactTxOutcome{}, err
	}
	// The go-evidence lock and recheck close the pass (#11327), apart from the owed
	// marker: the shared lock is held for a recheck and a commit, so a fence waits
	// milliseconds. No statement above writes, row-locks or table-locks fact_work_items
	// or writes or row-locks a scope or generation row (the pre-lock classifier and the
	// storage census pin this), so this wait cannot close a cycle with a fence holding the exclusive lock.
	locked, wait, err := closeTimedGoEvidence(ctx, tx, write, owed, timings)
	outcome.goEvidenceLocked, outcome.goEvidenceLockWait = locked, wait
	if err != nil {
		return supplyChainImpactTxOutcome{goEvidenceLocked: locked, goEvidenceLockWait: wait}, err
	}
	return outcome, nil
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
// this form always gets (#6831).
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
// The tombstoned rows' keys are staged by the same statement (#9805, routed
// through the staging queue by #10837), which reports two CTE-based counts:
// the rows it tombstoned and the distinct keys it marked. A retracted row is
// always a change. The DISTINCT collapses twin findings that share one key to
// one mark, and the maintainer tick later collapses staged marks into the
// versioned dirty log (version bumps by mark count). The caller keeps the
// tombstoned count as FactsRetracted (prior finding rows, per the write-result
// contract) and the marked count as the fact_retract capture count (staged
// rows touched).
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
        COALESCE(payload->>'advisory_id', '') AS advisory_id` + retractedFindingKeysReturningSQL + `
),
ordered AS (
    SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
    FROM retracted
    ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
),
upserted AS (
    INSERT INTO supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT 'fact', 'fact_retract', package_id, repository_id, subject_digest, c0, advisory_id
    FROM ordered
    RETURNING 1
)` + retractedFindingKeysRecordSQL + `
SELECT (SELECT count(*) FROM retracted) AS tombstoned,
       (SELECT count(*) FROM upserted) AS marked
`

// retractSupersededSupplyChainImpactFindingsScopedQuery is the scoped-pass
// (#9779) twin of the replace-set retraction above: same CTE-based
// tombstoned/marked counts, but the statement only tombstones rows inside the
// stamped CVE set. Rows outside it are not the pass's to retire, so they are
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
        COALESCE(payload->>'advisory_id', '') AS advisory_id` + retractedFindingKeysReturningSQL + `
),
ordered AS (
    SELECT DISTINCT package_id, repository_id, subject_digest, c0, advisory_id
    FROM retracted
    ORDER BY package_id, repository_id, subject_digest, c0, advisory_id
),
upserted AS (
    INSERT INTO supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT 'fact', 'fact_retract', package_id, repository_id, subject_digest, c0, advisory_id
    FROM ordered
    RETURNING 1
)` + retractedFindingKeysRecordSQL + `
SELECT (SELECT count(*) FROM retracted) AS tombstoned,
       (SELECT count(*) FROM upserted) AS marked
`

// retractSupersededSupplyChainImpactFindings runs the replace-set retraction
// for one pass and returns its CTE-based counts: the prior finding rows it
// tombstoned, and the distinct dirty-log keys it marked. A scoped pass
// (#9779) retracts inside its stamped CVE set only.
func retractSupersededSupplyChainImpactFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	scopeID string,
	generationID string,
	keepFactIDs []string,
	fencingToken int64,
	scopedCVEIDs []string,
) (tombstoned, marked int, err error) {
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
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, fmt.Errorf("retract superseded supply chain impact findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tombstoned64, marked64 int64
	found := false
	for rows.Next() {
		if err := rows.Scan(&tombstoned64, &marked64); err != nil {
			return 0, 0, fmt.Errorf("read supply chain impact retraction counts: %w", err)
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("read supply chain impact retraction counts: %w", err)
	}
	if !found {
		return 0, 0, fmt.Errorf("retract superseded supply chain impact findings: no count row")
	}
	return int(tombstoned64), int(marked64), nil
}
