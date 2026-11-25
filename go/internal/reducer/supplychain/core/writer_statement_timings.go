// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the per-statement write-transaction timing (#11430): the
// closed statement set, the pass's accumulation, and the timed phase helpers
// the statement runner calls. Only clock reads and struct stores run inside
// the transaction; the writer records the accumulation after the commit or
// rollback returns, beside RecordSupplyChainImpactWriteTx.

// supplyChainImpactTxOutcome is what one committed pass reports: how many rows
// it retracted, which CVEs its rows changed, which groups it touched (#10408),
// and, for a recovery write, every CVE the scope's rows name. suppressionCVEs
// are the changed suppressions' CVEs (#10019); suppression is the diff report.
type supplyChainImpactTxOutcome struct {
	retracted       int
	changedCVEs     []string
	derivedCVEs     []string
	recoveryCVEs    []string
	rekey           RekeyReport
	suppressionCVEs []string
	// touchedGroups are the coalesced advisory-group ids (#10411) the
	// pass's changed rows, changed suppressions, and (on recovery) every
	// stored row name. See SupplyChainImpactWriteResult.TouchedGroupIDs.
	touchedGroups []string
	// identityChangedCVEs and identityTouchedGroups are changedCVEs and
	// touchedGroups judged on the identity digest (D7, fanout_identity_gate.go).
	identityChangedCVEs, identityTouchedGroups []string
	suppression                                suppressionDiffReport
	// passStateSkipped reports the generation fence held (#10422): the
	// pass's generation was stale, so the per-scope pass-state row was
	// left untouched.
	passStateSkipped bool
	// staleCopyRepairs counts the stored copies whose retraction state this
	// pass flipped (#10765); the handler counts them after the commit.
	staleCopyRepairs StaleCopyRepairs
	// goEvidenceLocked/Wait say whether the pass took the shared lock and how long its
	// lock statements took (#11327); the writer records them on the lock-wait histogram, even on a failed recheck.
	goEvidenceLocked   bool
	goEvidenceLockWait time.Duration
	// statements accumulates the per-statement-group wall time of the pass's
	// write transaction (#11430): one sample per entered group, the chunked
	// fact insert aggregated to one. Only clock reads and struct stores run
	// inside the transaction; the writer records the accumulation after the
	// commit or rollback returns, beside RecordSupplyChainImpactWriteTx, so
	// the timing adds no logging or metric IO to the tx. A failed pass keeps
	// the groups that ran, recorded under outcome=rolled_back.
	statements supplyChainImpactTxStatementTimings
}

// supplyChainImpactTxStatement names one timed region of the impact write
// transaction, in emission order. The order matches
// telemetry.SupplyChainImpactWriteTxStatementLabels.
type supplyChainImpactTxStatement int

const (
	supplyChainImpactTxStmtConflictLock supplyChainImpactTxStatement = iota
	supplyChainImpactTxStmtSuppressionDiff
	supplyChainImpactTxStmtAdmission
	supplyChainImpactTxStmtStoredRead
	supplyChainImpactTxStmtFactInsert
	supplyChainImpactTxStmtSupportsUpsert
	supplyChainImpactTxStmtWinnersCapture
	supplyChainImpactTxStmtRetract
	supplyChainImpactTxStmtSupportsDelete
	supplyChainImpactTxStmtRecoveryRead
	supplyChainImpactTxStmtPassState
	supplyChainImpactTxStmtGoEvidenceClose
	supplyChainImpactTxStmtCommit
	supplyChainImpactTxStmtCount
)

// supplyChainImpactTxStatementLabels maps each timed region to its closed
// telemetry label.
var supplyChainImpactTxStatementLabels = [supplyChainImpactTxStmtCount]string{
	telemetry.SupplyChainImpactWriteTxStatementConflictLock,
	telemetry.SupplyChainImpactWriteTxStatementSuppressionDiff,
	telemetry.SupplyChainImpactWriteTxStatementAdmission,
	telemetry.SupplyChainImpactWriteTxStatementStoredRead,
	telemetry.SupplyChainImpactWriteTxStatementFactInsert,
	telemetry.SupplyChainImpactWriteTxStatementSupportsUpsert,
	telemetry.SupplyChainImpactWriteTxStatementWinnersCapture,
	telemetry.SupplyChainImpactWriteTxStatementRetract,
	telemetry.SupplyChainImpactWriteTxStatementSupportsDelete,
	telemetry.SupplyChainImpactWriteTxStatementRecoveryRead,
	telemetry.SupplyChainImpactWriteTxStatementPassState,
	telemetry.SupplyChainImpactWriteTxStatementGoEvidenceClose,
	telemetry.SupplyChainImpactWriteTxStatementCommit,
}

// supplyChainImpactTxStatementSample is one region's accumulation: its total
// wall time, how many timed calls it aggregated (chunks for the fact insert),
// and the rows those calls carried.
type supplyChainImpactTxStatementSample struct {
	seconds float64
	calls   int
	rows    int64
}

// supplyChainImpactTxStatementTimings accumulates one pass's per-region wall
// time. It is written by the writer goroutine only, so it needs no lock.
type supplyChainImpactTxStatementTimings struct {
	samples [supplyChainImpactTxStmtCount]supplyChainImpactTxStatementSample
	present [supplyChainImpactTxStmtCount]bool
}

// add folds one timed call into the region's accumulation. It is safe on a nil
// receiver, so callers never branch on whether timing is enabled.
func (t *supplyChainImpactTxStatementTimings) add(stmt supplyChainImpactTxStatement, d time.Duration, rows int64) {
	if t == nil {
		return
	}
	t.samples[stmt].seconds += d.Seconds()
	t.samples[stmt].calls++
	t.samples[stmt].rows += rows
	t.present[stmt] = true
}

// sample reports one region's accumulation: total seconds, timed calls, rows,
// and whether the region ran.
func (t supplyChainImpactTxStatementTimings) sample(stmt supplyChainImpactTxStatement) (seconds float64, calls int, rows int64, ok bool) {
	if !t.present[stmt] {
		return 0, 0, 0, false
	}
	s := t.samples[stmt]
	return s.seconds, s.calls, s.rows, true
}

// toTelemetry converts the accumulation to emission order, skipping the
// regions the pass never entered.
func (t supplyChainImpactTxStatementTimings) toTelemetry() []telemetry.SupplyChainImpactWriteTxStatementTiming {
	var out []telemetry.SupplyChainImpactWriteTxStatementTiming
	for stmt := supplyChainImpactTxStatement(0); stmt < supplyChainImpactTxStmtCount; stmt++ {
		if !t.present[stmt] {
			continue
		}
		out = append(out, telemetry.SupplyChainImpactWriteTxStatementTiming{
			Statement: supplyChainImpactTxStatementLabels[stmt],
			Seconds:   t.samples[stmt].seconds,
		})
	}
	return out
}

// supplyChainImpactWriteTxStatementOutcome maps a pass result to the closed
// statement-histogram outcome: committed, or rolled_back for every failure. A
// go-evidence recheck abort rolls its transaction back like any other failure,
// so it records rolled_back here even though the whole-tx histogram keeps its
// own recheck_aborted series.
func supplyChainImpactWriteTxStatementOutcome(err error) string {
	if err == nil {
		return "committed"
	}
	return "rolled_back"
}

// commitSupplyChainImpactRows runs one pass's statements inside tx and returns
// what it retracted and changed. The per-statement timings accumulate on the
// returned outcome; a failed pass keeps the groups that ran.
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
	var timings supplyChainImpactTxStatementTimings
	outcome, err := commitSupplyChainImpactRowsTimed(ctx, tx, write, rows, meta, protected, hasProtected, now, &timings)
	outcome.statements = timings
	return outcome, err
}

// captureTimedWinnersDirtyKeys stages the identifiers the pass wrote when a
// capturer is present, folding the statement into the pass's winners_capture
// timing. A pass that names nothing to capture, or runs on a transaction
// without a capturer, records nothing.
func captureTimedWinnersDirtyKeys(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	meta []changedRow,
	stored []storedFinding,
	keep []string,
	timings *supplyChainImpactTxStatementTimings,
) error {
	capturer, ok := tx.(SupplyChainImpactWinnersCapturer)
	if !ok {
		return nil
	}
	idents := dirtyIdentifiersToCapture(meta, stored, write.GenerationID)
	if len(idents) == 0 {
		return nil
	}
	idents = excludeRetiredWinnersDirtyIdentifiers(idents, stored, keep, write.GenerationID, write.FencingToken, write.PartialEvidence, write.ScopedCVEIDs)
	if len(idents) == 0 {
		return nil
	}
	captureStarted := time.Now()
	captured, err := capturer.CaptureWinnersDirtyFacts(ctx, idents)
	timings.add(supplyChainImpactTxStmtWinnersCapture, time.Since(captureStarted), captured)
	if err != nil {
		return fmt.Errorf("capture supply chain impact winners dirty keys: %w", err)
	}
	return nil
}

// retractTimedSupersededFindings runs the replace-set retraction and, when the
// transaction maintains supports, the supports delete of the tombstoned facts,
// folding both into the pass's timings. It returns the tombstoned row count.
func retractTimedSupersededFindings(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	keep []string,
	supports SupplyChainImpactSupportsMaintainer,
	hasSupports bool,
	timings *supplyChainImpactTxStatementTimings,
) (int, error) {
	retractStarted := time.Now()
	tombstoned, marked, err := retractSupersededSupplyChainImpactFindings(ctx, tx, write.ScopeID, write.GenerationID, keep, write.FencingToken, write.ScopedCVEIDs)
	timings.add(supplyChainImpactTxStmtRetract, time.Since(retractStarted), int64(tombstoned))
	if err != nil {
		return 0, err
	}
	if hasSupports {
		supportsDeleteStarted := time.Now()
		deleted, supportsDeleteErr := supports.DeleteRetractedImpactSupports(ctx, write.ScopeID, write.GenerationID)
		timings.add(supplyChainImpactTxStmtSupportsDelete, time.Since(supportsDeleteStarted), deleted)
		if supportsDeleteErr != nil {
			return 0, fmt.Errorf("remove retracted supply chain impact supports: %w", supportsDeleteErr)
		}
	}
	// The capture count is the marked keys, not the tombstoned rows:
	// twin findings that share one key collapse to one staged mark. It rides
	// the retraction whether or not this transaction maintains supports.
	if capturer, ok := tx.(SupplyChainImpactWinnersCapturer); ok {
		capturer.RecordWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceFactRetract, int64(marked))
	}
	return tombstoned, nil
}

// readTimedRecoveryReport reads the recovery CVE and advisory-group ids the
// scope's rows name, folding both reads into the pass's recovery_read timing.
func readTimedRecoveryReport(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	timings *supplyChainImpactTxStatementTimings,
) (cves, groups []string, err error) {
	recoveryCVEIDsStarted := time.Now()
	cves, err = readSupplyChainImpactCVEIDs(ctx, tx, write.ScopeID, write.GenerationID)
	timings.add(supplyChainImpactTxStmtRecoveryRead, time.Since(recoveryCVEIDsStarted), int64(len(cves)))
	if err != nil {
		return nil, nil, err
	}
	recoveryGroupIDsStarted := time.Now()
	groups, err = readSupplyChainImpactGroupIDs(ctx, tx, write.ScopeID, write.GenerationID)
	timings.add(supplyChainImpactTxStmtRecoveryRead, time.Since(recoveryGroupIDsStarted), int64(len(groups)))
	if err != nil {
		return nil, nil, err
	}
	return cves, groups, nil
}

// closeTimedGoEvidence runs the closing go-evidence statements — the shared
// locks, the recheck, and the owed marker when the pass owes one — folding
// them into the pass's go_evidence_close timing.
func closeTimedGoEvidence(
	ctx context.Context,
	tx SupplyChainImpactTx,
	write SupplyChainImpactWrite,
	owed bool,
	timings *supplyChainImpactTxStatementTimings,
) (locked bool, wait time.Duration, err error) {
	goEvidenceCloseStarted := time.Now()
	locked, wait, err = lockAndRecheckGoEvidence(ctx, tx, write.GoEvidence)
	timings.add(supplyChainImpactTxStmtGoEvidenceClose, time.Since(goEvidenceCloseStarted), 0)
	if err == nil && owed {
		owedStarted := time.Now()
		wait, err = recordOwedAfterGoEvidenceLock(ctx, tx, write, locked, wait)
		timings.add(supplyChainImpactTxStmtGoEvidenceClose, time.Since(owedStarted), 0)
	}
	return locked, wait, err
}
