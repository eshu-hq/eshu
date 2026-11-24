// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// Per-support table row actions for eshu_dp_supply_chain_impact_supports_rows_total.
// They are the closed set of the action label.
const (
	// SupplyChainImpactSupportsActionInserted counts a support row created.
	SupplyChainImpactSupportsActionInserted = "inserted"
	// SupplyChainImpactSupportsActionUpdated counts a support row whose derived
	// columns changed and was rewritten.
	SupplyChainImpactSupportsActionUpdated = "updated"
	// SupplyChainImpactSupportsActionUnchanged counts a support the write-on-change
	// guard left alone (a replay of an unchanged finding).
	SupplyChainImpactSupportsActionUnchanged = "unchanged"
	// SupplyChainImpactSupportsActionDeleted counts a support row removed with
	// the fact a retraction tombstoned.
	SupplyChainImpactSupportsActionDeleted = "deleted"
)

// Per-support table writers for the source label of
// eshu_dp_supply_chain_impact_supports_rows_total.
const (
	// SupplyChainImpactSupportsSourceWriter is the impact writer's transaction.
	SupplyChainImpactSupportsSourceWriter = "writer"
	// SupplyChainImpactSupportsSourceBackfill is the reducer-owned backfill.
	SupplyChainImpactSupportsSourceBackfill = "backfill"
)

// Supports backfill step outcomes for
// eshu_dp_supply_chain_impact_supports_backfill_steps_total. They are the closed
// set of the outcome label.
const (
	// SupplyChainImpactSupportsBackfillStepChunk is a step that wrote or cleaned
	// supports of one (scope, generation).
	SupplyChainImpactSupportsBackfillStepChunk = "chunk"
	// SupplyChainImpactSupportsBackfillStepGap is a verification that found live
	// facts without a support and restarted the sweep.
	SupplyChainImpactSupportsBackfillStepGap = "verify_gap"
	// SupplyChainImpactSupportsBackfillStepDrain is a step that re-read a scope the
	// old-writer fence marked dirty.
	SupplyChainImpactSupportsBackfillStepDrain = "drain"
	// SupplyChainImpactSupportsBackfillStepComplete is the step that set the
	// readiness marker.
	SupplyChainImpactSupportsBackfillStepComplete = "complete"
	// SupplyChainImpactSupportsBackfillStepIdle is a step after completion.
	SupplyChainImpactSupportsBackfillStepIdle = "idle"
	// SupplyChainImpactSupportsBackfillStepContended is a pass another reducer
	// replica held the backfill lock for.
	SupplyChainImpactSupportsBackfillStepContended = "contended"
	// SupplyChainImpactSupportsBackfillStepFailed is a step that returned an error.
	SupplyChainImpactSupportsBackfillStepFailed = "failed"
	// SupplyChainImpactSupportsBackfillStepVerify is a verification slice that
	// neither restarted the sweep nor set the marker: it moved the cursor, removed
	// an orphan pair, or was a last slice the fence recheck held back. The slice
	// counter's outcome label says which.
	SupplyChainImpactSupportsBackfillStepVerify = "verify"
)

// Verification slice outcomes for
// eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total and its
// duration histogram. They are the closed set of the outcome label.
const (
	// SupplyChainImpactSupportsVerifySliceAdvance is a slice that found no
	// missing support and no orphan and moved the cursor to its end.
	SupplyChainImpactSupportsVerifySliceAdvance = "advance"
	// SupplyChainImpactSupportsVerifySliceOrphan is a slice that found a support
	// of a tombstoned fact; the step removed that pair's orphans and kept the
	// cursor, so the slice runs again.
	SupplyChainImpactSupportsVerifySliceOrphan = "orphan"
	// SupplyChainImpactSupportsVerifySliceGap is a slice that found a live finding
	// without a support and restarted the sweep.
	SupplyChainImpactSupportsVerifySliceGap = "gap"
	// SupplyChainImpactSupportsVerifySliceLast is the clean slice that reached the
	// end of the key space and ran the completion statement.
	SupplyChainImpactSupportsVerifySliceLast = "last"
	// SupplyChainImpactSupportsVerifySliceFailed is a slice whose statement, or
	// the restart, cleanup, advance or completion after it, returned an error (a
	// statement timeout among them); the step fails and the slice runs again.
	SupplyChainImpactSupportsVerifySliceFailed = "failed"
)

// Reducer reads of the per-support table (#10880 PR-B2) for the reader
// label (carried as the source dimension, a closed set) and outcome of
// eshu_dp_supply_chain_impact_supports_reads_total.
const (
	// SupplyChainImpactSupportsReaderSecurityAlertReconciliation is the
	// security-alert reconciliation evidence loader.
	SupplyChainImpactSupportsReaderSecurityAlertReconciliation = "security_alert_reconciliation"
	// SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory is the
	// service-catalog vulnerability loader.
	SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory = "service_vulnerability_advisory"
	// SupplyChainImpactSupportsReaderFanoutHolders is the impact pass's
	// reopen fan-out holder read.
	SupplyChainImpactSupportsReaderFanoutHolders = "fanout_holders"
	// SupplyChainImpactSupportsReaderFanoutHoldersKeyRange is the fan-out's
	// key-range holder read.
	SupplyChainImpactSupportsReaderFanoutHoldersKeyRange = "fanout_holders_key_range"
	// SupplyChainImpactSupportsReaderFanoutHolderMass is the fan-out's
	// mass-probe holder read.
	SupplyChainImpactSupportsReaderFanoutHolderMass = "fanout_holder_mass"
	// SupplyChainImpactSupportsReaderRepositoryHolders is the repository
	// holder read.
	SupplyChainImpactSupportsReaderRepositoryHolders = "repository_holders"
	// SupplyChainImpactSupportsReaderScopedPackages is the scoped-package
	// holder read.
	SupplyChainImpactSupportsReaderScopedPackages = "scoped_packages"
	// SupplyChainImpactSupportsReaderOutOfStamp is the out-of-stamp holder
	// read.
	SupplyChainImpactSupportsReaderOutOfStamp = "out_of_stamp"
	// SupplyChainImpactSupportsReaderGoModulesFence is the Go-modules fence's
	// holder read.
	SupplyChainImpactSupportsReaderGoModulesFence = "go_modules_fence"
)

// Outcomes of a reducer read of the per-support table. They are the closed set
// of the outcome label of eshu_dp_supply_chain_impact_supports_reads_total.
const (
	// SupplyChainImpactSupportsReadServed is a read that loaded every page.
	SupplyChainImpactSupportsReadServed = "served"
	// SupplyChainImpactSupportsReadNotReady is a read the reader refused because
	// the supports table was not ready; the intent retries.
	SupplyChainImpactSupportsReadNotReady = "not_ready"
	// SupplyChainImpactSupportsReadFailed is a read that failed on the readiness
	// probe or a page load.
	SupplyChainImpactSupportsReadFailed = "failed"
)

// supplyChainImpactSupportsBackfillStepBuckets spans a step from a few
// milliseconds (an idle marker read) to the minute a very large chunk under
// contention could take, since an operator reads the tail as how long a writer
// may wait on the backfill's advisory lock.
var supplyChainImpactSupportsBackfillStepBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// supplyChainImpactSupportsDrainRowBuckets spans a drain from a handful of rows to
// the millions a hot package could hold.
var supplyChainImpactSupportsDrainRowBuckets = []float64{0, 1, 10, 100, 1000, 10000, 100000, 1000000, 10000000}

// supplyChainImpactSupportsDrainDurationBuckets spans a drain from a few
// milliseconds (one page) to the hour a drain of a very hot key could take.
var supplyChainImpactSupportsDrainDurationBuckets = []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 3600}

// registerSupplyChainImpactSupportsInstruments registers the per-support table
// signals of #10880 PR-B1 on inst.
//
//   - eshu_dp_supply_chain_impact_supports_rows_total{action,source}: support
//     rows the impact writer's transaction (counted after it commits, so a
//     rollback adds nothing) or the backfill wrote. A writer whose inserted and
//     updated stay flat while findings are written is a maintainer that stopped.
//
//   - eshu_dp_supply_chain_impact_supports_backfill_steps_total{outcome}: backfill
//     steps by outcome. A rising verify_gap count means supports are going
//     missing after the sweep passed them (a writer without the maintainer).
//
//   - eshu_dp_supply_chain_impact_supports_backfill_step_duration_seconds{outcome},
//     ..._dirty_scopes and ..._last_success_timestamp_seconds: how long a step
//     holds a lock, the old-writer fence depth, and when the loop last worked.
//
//   - eshu_dp_supply_chain_impact_supports_reads_total{source,outcome}: reducer
//     reads of the supports table by reader (carried as source) and outcome
//     (served, not_ready, failed). A rising not_ready count means a reducer
//     intent is waiting on the backfill; it clears when the ready gauge reads 1.
//
//   - eshu_dp_supply_chain_impact_supports_drain_raw_rows{source}: raw supports
//     rows one drain read (one whole call for the service-catalog reader; every
//     row the ordered key scans returned, active or not, including the rows a failed
//     drain read before it stopped), by reader. A drain whose raw rows run far above its active
//     rows is reading superseded generations; a drain in the hundred thousands is
//     a hot key.
//
//   - eshu_dp_supply_chain_impact_supports_drain_duration_seconds{source}: wall time
//     of one drain (one whole call for the service-catalog reader), by reader. A drain near half the domain's handler
//     timeout is the signal to return the hot-key page contract to its owner.
//
//   - eshu_dp_supply_chain_impact_supports_backfill_ready: 1 once the readiness
//     marker is set at the current contract version and the fence is empty, else 0.
//
//   - eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total{outcome}
//     and ..._verify_slice_duration_seconds{outcome}: the bounded verification
//     slices and the wall time of each slice statement. The count of advance
//     slices is how far the verification got; the duration tail is the longest
//     snapshot the verification holds.
//
// No label carries a scope, repository, canonical key or CVE.
func registerSupplyChainImpactSupportsInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactSupportsRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_rows_total",
		metric.WithDescription("Per-support table rows written, by action (inserted, updated, unchanged, deleted) and source (writer, backfill). The impact writer's rows count after its transaction commits."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsRows counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsReads, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_reads_total",
		metric.WithDescription("Reducer reads of the per-support table by reader (source label) and outcome (served, not_ready, failed). A rising not_ready count means a reducer intent is waiting on the supports backfill."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsReads counter: %w", err)
	}
	if inst.SupplyChainImpactFactWriteRejected, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_fact_write_rejected_total",
		metric.WithDescription("Supply-chain impact finding fact writes the reject trigger refused (SQLSTATE 55000, constraint fact_records_reject_supply_chain_impact_finding). Each one is a reducer on an image that still writes the kind after the write stop; the intent retries without spending an attempt. No labels. A count above zero means an old reducer image is still running."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactFactWriteRejected counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsDrainRawRows, err = meter.Int64Histogram(
		"eshu_dp_supply_chain_impact_supports_drain_raw_rows",
		metric.WithDescription("Raw per-support rows one drain read (one whole call for the service-catalog reader; active or not, and the rows a failed drain read before it stopped), by reader (source label). Raw rows far above the active rows mean the key's supports sit mostly in superseded generations; a drain in the hundred thousands is a hot key."),
		metric.WithExplicitBucketBoundaries(supplyChainImpactSupportsDrainRowBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsDrainRawRows histogram: %w", err)
	}
	if inst.SupplyChainImpactSupportsDrainDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_supports_drain_duration_seconds",
		metric.WithDescription("Wall time of one drain of the per-support table (one whole call for the service-catalog reader), by reader (source label). A drain near half the domain's handler timeout means a hot key is paging too slowly."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactSupportsDrainDurationBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsDrainDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillSteps, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_backfill_steps_total",
		metric.WithDescription("Per-support backfill steps by outcome (chunk, verify, verify_gap, drain, complete, idle, contended, failed). A rising verify_gap count means supports went missing after the sweep passed them."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillSteps counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillStepDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_supports_backfill_step_duration_seconds",
		metric.WithDescription("Wall time of one per-support backfill step, by outcome. A step holds one scope-and-generation advisory lock for at most one chunk, so the tail is the longest a writer can wait on the backfill."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactSupportsBackfillStepBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillStepDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactSupportsDirtyScopes, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_supports_dirty_scopes",
		metric.WithDescription("Scopes the old-writer fence marked dirty and the backfill has not drained yet, capped at 1000. Non-zero holds the table not ready; a count that never falls means writers without the maintainer are still running."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsDirtyScopes gauge: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillLastSuccess, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_supports_backfill_last_success_timestamp_seconds",
		metric.WithDescription("Unix time of the last per-support backfill step that finished without error."),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillLastSuccess gauge: %w", err)
	}
	if inst.SupplyChainImpactSupportsBackfillReady, err = meter.Int64Gauge(
		"eshu_dp_supply_chain_impact_supports_backfill_ready",
		metric.WithDescription("1 once the per-support backfill finished and verified at the current contract version, else 0."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsBackfillReady gauge: %w", err)
	}
	if inst.SupplyChainImpactSupportsVerifySlices, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total",
		metric.WithDescription("Per-support backfill verification slices by outcome (advance, orphan, gap, last, failed). Each slice checks one bounded key interval of supports and live findings in one snapshot."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsVerifySlices counter: %w", err)
	}
	if inst.SupplyChainImpactSupportsVerifySliceDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds",
		metric.WithDescription("Wall time of one per-support backfill verification slice statement, by outcome. The tail is the longest snapshot the verification holds open."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactSupportsBackfillStepBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactSupportsVerifySliceDuration histogram: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactSupportsVerifySlice counts one verification slice under
// outcome, which must be one of the SupplyChainImpactSupportsVerifySlice*
// constants, and records the slice statement's wall time. It is safe on a nil
// receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsVerifySlice(ctx context.Context, outcome string, spent time.Duration) {
	if i == nil {
		return
	}
	attrs := metric.WithAttributes(AttrOutcome(outcome))
	if i.SupplyChainImpactSupportsVerifySlices != nil {
		i.SupplyChainImpactSupportsVerifySlices.Add(ctx, 1, attrs)
	}
	if i.SupplyChainImpactSupportsVerifySliceDuration != nil {
		i.SupplyChainImpactSupportsVerifySliceDuration.Record(ctx, spent.Seconds(), attrs)
	}
}

// RecordSupplyChainImpactSupportsRows counts support rows written under action
// by source. A zero count records nothing. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsRows(ctx context.Context, action, source string, rows int64) {
	if i == nil || i.SupplyChainImpactSupportsRows == nil || rows <= 0 {
		return
	}
	i.SupplyChainImpactSupportsRows.Add(ctx, rows, metric.WithAttributes(AttrAction(action), AttrSource(source)))
}

// RecordSupplyChainImpactFactWriteRejected counts one impact-finding fact write
// the reject trigger refused. It carries no label, so no scope, generation or fact
// id can reach the metric. It is safe on a nil receiver and on an Instruments that
// did not register the counter.
func (i *Instruments) RecordSupplyChainImpactFactWriteRejected(ctx context.Context) {
	if i == nil || i.SupplyChainImpactFactWriteRejected == nil {
		return
	}
	i.SupplyChainImpactFactWriteRejected.Add(ctx, 1)
}

// RecordSupplyChainImpactSupportsRead counts one reducer read of the supports
// table by reader and outcome, which must be one of the
// SupplyChainImpactSupportsRead* constants. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsRead(ctx context.Context, reader, outcome string) {
	if i == nil || i.SupplyChainImpactSupportsReads == nil {
		return
	}
	i.SupplyChainImpactSupportsReads.Add(ctx, 1, metric.WithAttributes(AttrSource(reader), AttrOutcome(outcome)))
}

// RecordSupplyChainImpactSupportsDrainRawRows records the raw supports rows one
// drain read (one whole call for the service-catalog reader, including the rows a
// failed drain read before it stopped), by reader (one of the SupplyChainImpactSupportsReader*
// constants). It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsDrainRawRows(ctx context.Context, reader string, rawRows int64) {
	if i == nil || i.SupplyChainImpactSupportsDrainRawRows == nil || rawRows < 0 {
		return
	}
	i.SupplyChainImpactSupportsDrainRawRows.Record(ctx, rawRows, metric.WithAttributes(AttrSource(reader)))
}

// RecordSupplyChainImpactSupportsDrainDuration records the wall time of one
// drain of the supports table (one whole call for the service-catalog reader), by
// reader. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsDrainDuration(ctx context.Context, reader string, d time.Duration) {
	if i == nil || i.SupplyChainImpactSupportsDrainDuration == nil {
		return
	}
	i.SupplyChainImpactSupportsDrainDuration.Record(ctx, d.Seconds(), metric.WithAttributes(AttrSource(reader)))
}

// RecordSupplyChainImpactSupportsBackfillStep counts one backfill step under
// outcome, which must be one of the SupplyChainImpactSupportsBackfillStep*
// constants. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsBackfillStep(ctx context.Context, outcome string) {
	if i == nil || i.SupplyChainImpactSupportsBackfillSteps == nil {
		return
	}
	i.SupplyChainImpactSupportsBackfillSteps.Add(ctx, 1, metric.WithAttributes(AttrOutcome(outcome)))
}

// RecordSupplyChainImpactSupportsBackfillStepResult records a step's wall time
// and, on success, the dirty-scope depth and the last-success time. A nil receiver
// records nothing.
func (i *Instruments) RecordSupplyChainImpactSupportsBackfillStepResult(ctx context.Context, outcome string, spent time.Duration, dirtyScopes int64) {
	if i == nil {
		return
	}
	if i.SupplyChainImpactSupportsBackfillStepDuration != nil {
		i.SupplyChainImpactSupportsBackfillStepDuration.Record(ctx, spent.Seconds(), metric.WithAttributes(AttrOutcome(outcome)))
	}
	if i.SupplyChainImpactSupportsDirtyScopes != nil {
		i.SupplyChainImpactSupportsDirtyScopes.Record(ctx, dirtyScopes)
	}
	if i.SupplyChainImpactSupportsBackfillLastSuccess != nil {
		i.SupplyChainImpactSupportsBackfillLastSuccess.Record(ctx, time.Now().Unix())
	}
}

// RecordSupplyChainImpactSupportsBackfillReady sets the readiness gauge. It is
// safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactSupportsBackfillReady(ctx context.Context, ready bool) {
	if i == nil || i.SupplyChainImpactSupportsBackfillReady == nil {
		return
	}
	var value int64
	if ready {
		value = 1
	}
	i.SupplyChainImpactSupportsBackfillReady.Record(ctx, value)
}
