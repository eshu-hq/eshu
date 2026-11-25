// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Per-statement write-transaction groups for
// eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds (#11430).
// They are the closed set of the statement label: one series per pass per
// entered group, never one series per chunk, and no scope, generation, CVE,
// fact id or repository reaches the label.
const (
	// SupplyChainImpactWriteTxStatementConflictLock is the conflict-domain
	// advisory lock that serializes one (scope, generation).
	SupplyChainImpactWriteTxStatementConflictLock = "conflict_lock"
	// SupplyChainImpactWriteTxStatementSuppressionDiff is the suppression
	// diff read and walk inside the write transaction.
	SupplyChainImpactWriteTxStatementSuppressionDiff = "suppression_diff"
	// SupplyChainImpactWriteTxStatementAdmission is the fencing-token
	// admission check that rejects a stale pass whole.
	SupplyChainImpactWriteTxStatementAdmission = "admission"
	// SupplyChainImpactWriteTxStatementStoredRead is the read of the stored
	// rows the pass is about to overwrite.
	SupplyChainImpactWriteTxStatementStoredRead = "stored_read"
	// SupplyChainImpactWriteTxStatementFactInsert is the chunked finding-fact
	// upsert, aggregated to one sample per pass no matter how many chunks it
	// took.
	SupplyChainImpactWriteTxStatementFactInsert = "fact_insert"
	// SupplyChainImpactWriteTxStatementSupportsUpsert is the per-support
	// upsert of the fact ids the pass just wrote.
	SupplyChainImpactWriteTxStatementSupportsUpsert = "supports_upsert"
	// SupplyChainImpactWriteTxStatementWinnersCapture is the winners dirty-key
	// staging of the identifiers the pass wrote.
	SupplyChainImpactWriteTxStatementWinnersCapture = "winners_capture"
	// SupplyChainImpactWriteTxStatementRetract is the replace-set retraction
	// that tombstones the rows the pass did not rewrite.
	SupplyChainImpactWriteTxStatementRetract = "retract"
	// SupplyChainImpactWriteTxStatementSupportsDelete is the removal of the
	// supports of the facts the retraction tombstoned.
	SupplyChainImpactWriteTxStatementSupportsDelete = "supports_delete"
	// SupplyChainImpactWriteTxStatementRecoveryRead is the recovery report of
	// every CVE and advisory-group id the scope's rows name.
	SupplyChainImpactWriteTxStatementRecoveryRead = "recovery_read"
	// SupplyChainImpactWriteTxStatementPassState is the durable per-scope
	// pass-state record.
	SupplyChainImpactWriteTxStatementPassState = "pass_state"
	// SupplyChainImpactWriteTxStatementGoEvidenceClose is the closing
	// go-evidence shared locks, recheck and owed marker.
	SupplyChainImpactWriteTxStatementGoEvidenceClose = "go_evidence_close"
	// SupplyChainImpactWriteTxStatementCommit is the transaction commit
	// itself, timed on every pass, committed or not.
	SupplyChainImpactWriteTxStatementCommit = "commit"
)

// SupplyChainImpactWriteTxStatementLabels returns the closed statement label
// set in emission order.
func SupplyChainImpactWriteTxStatementLabels() []string {
	return []string{
		SupplyChainImpactWriteTxStatementConflictLock,
		SupplyChainImpactWriteTxStatementSuppressionDiff,
		SupplyChainImpactWriteTxStatementAdmission,
		SupplyChainImpactWriteTxStatementStoredRead,
		SupplyChainImpactWriteTxStatementFactInsert,
		SupplyChainImpactWriteTxStatementSupportsUpsert,
		SupplyChainImpactWriteTxStatementWinnersCapture,
		SupplyChainImpactWriteTxStatementRetract,
		SupplyChainImpactWriteTxStatementSupportsDelete,
		SupplyChainImpactWriteTxStatementRecoveryRead,
		SupplyChainImpactWriteTxStatementPassState,
		SupplyChainImpactWriteTxStatementGoEvidenceClose,
		SupplyChainImpactWriteTxStatementCommit,
	}
}

// SupplyChainImpactWriteTxStatementTiming is one accumulated per-statement
// observation for a pass: the statement group and its wall time in seconds,
// already aggregated across chunks. The outcome travels beside the slice, not
// in it, so one slice serves the committed and the rolled-back emission.
type SupplyChainImpactWriteTxStatementTiming struct {
	Statement string
	Seconds   float64
}

// supplyChainImpactWriteTxStatementBuckets spans one statement group inside an
// impact write transaction from an uncontended advisory grant (sub-millisecond)
// to the 300 s ceiling a ~10k-row chunked upsert shares with the whole-tx
// buckets, so a per-statement tail reads against the same scale as the pass.
var supplyChainImpactWriteTxStatementBuckets = []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// registerSupplyChainImpactWriteTxStatementInstruments registers the
// per-statement impact write-transaction signal of #11430:
//
//   - eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds{statement,
//     outcome}: the wall time of one statement group inside one impact write
//     transaction. The fact_insert sample aggregates the whole chunked bulk
//     insert to one observation per pass; a rolled-back pass records the
//     groups that ran. Compare the committed fact_insert and commit series to
//     split the write_findings tail.
//
// No label carries a scope, repository, canonical key or CVE.
func registerSupplyChainImpactWriteTxStatementInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactWriteTxStatementDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_write_tx_statement_duration_seconds",
		metric.WithDescription("Wall time of one statement group inside one supply-chain impact write transaction, by statement (closed set: conflict_lock, suppression_diff, admission, stored_read, fact_insert, supports_upsert, winners_capture, retract, supports_delete, recovery_read, pass_state, go_evidence_close, commit) and outcome (committed, rolled_back). The fact_insert sample is one pass-level aggregate no matter how many bulk-insert chunks the pass took, and the commit sample is timed on every pass. A rolled-back pass records the groups that ran before the failure. Use the committed fact_insert and commit series to split the write_findings tail."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWriteTxStatementBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWriteTxStatementDuration histogram: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactWriteTxStatements records one pass's accumulated
// per-statement write-transaction timings under outcome (committed or
// rolled_back). Each entry carries exactly the statement and outcome labels,
// so the series stay in the closed set. A nil or empty slice records nothing.
// It is safe on a nil receiver and on an Instruments that did not register
// the instrument.
func (i *Instruments) RecordSupplyChainImpactWriteTxStatements(ctx context.Context, outcome string, statements []SupplyChainImpactWriteTxStatementTiming) {
	if i == nil || i.SupplyChainImpactWriteTxStatementDuration == nil {
		return
	}
	for _, statement := range statements {
		i.SupplyChainImpactWriteTxStatementDuration.Record(ctx, statement.Seconds,
			metric.WithAttributes(AttrStatement(statement.Statement), AttrOutcome(outcome)))
	}
}
