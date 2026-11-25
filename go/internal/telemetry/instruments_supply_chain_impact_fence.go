// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// goFenceAdvisoryLockWaitBuckets spans a fence wait from an uncontended grant
// (microseconds) to its 2 s lock timeout and a little past, so a timeout lands in
// the last finite bucket.
var goFenceAdvisoryLockWaitBuckets = []float64{0.0005, 0.001, 0.005, 0.025, 0.1, 0.25, 0.5, 1, 1.5, 2, 2.5, 5}

// supplyChainImpactWriteTxBuckets spans an impact write transaction from a few
// milliseconds (an unchanged replay) to past the minute a pass takes on a loaded
// database, so the tail that decides the fence's wait and the abort cost is visible.
var supplyChainImpactWriteTxBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300}

// supplyChainImpactGoEvidenceLockWaitBuckets spans a pass's wait for the shared
// go-evidence lock from an uncontended grant to a wait behind a fence and a few of
// the passes that queued ahead of it.
var supplyChainImpactGoEvidenceLockWaitBuckets = []float64{0.0005, 0.001, 0.005, 0.025, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60}

// registerSupplyChainImpactFenceInstruments registers the go-evidence fence
// signals of #11327 on inst, called from registerSupplyChainImpactSupportsInstruments.
//
//   - eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total{reason}: passes
//     aborted at the end of their write transaction because the Go evidence they
//     loaded went stale (reason generation_changed) or the shared go-evidence
//     lock timed out behind a fence (reason lock_timeout). Each one retries
//     without spending an attempt.
//
//   - eshu_dp_go_fence_advisory_lock_timeouts_total: Go-modules fence attempts
//     that hit the lock timeout waiting for the exclusive go-evidence advisory
//     lock behind an in-flight pass. Nothing was detached.
//
//   - eshu_dp_go_fence_advisory_lock_wait_seconds: wall time the fence waited
//     for the exclusive lock, granted or timed out. The tail is the longest
//     pass write transaction that held the shared lock.
//
//   - eshu_dp_supply_chain_impact_write_tx_duration_seconds{outcome}: wall time
//     of one impact write transaction, begin to commit or rollback, by outcome
//     (committed, rolled_back, recheck_aborted).
//
//   - eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds: wall time a
//     pass spent in the closing statements after its first shared go-evidence
//     advisory lock. Passes with no resolved Go scope record nothing.
//
// No label carries a scope, repository, canonical key or CVE.
func registerSupplyChainImpactFenceInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactGoEvidenceRecheckAborts, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total",
		metric.WithDescription("Supply-chain impact passes aborted at the end of their write transaction, so their writes rolled back, because the Go evidence they loaded no longer names its go scope's active generation (reason generation_changed) or the shared go-evidence lock timed out behind a fence (reason lock_timeout). Each one retries without spending an attempt; the retry reloads the evidence."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactGoEvidenceRecheckAborts counter: %w", err)
	}
	if inst.GoFenceAdvisoryLockTimeouts, err = meter.Int64Counter(
		"eshu_dp_go_fence_advisory_lock_timeouts_total",
		metric.WithDescription("Go-modules fence attempts that hit the lock timeout (SQLSTATE 55P03) waiting for the exclusive go-evidence advisory lock behind an in-flight supply-chain impact pass. Nothing was detached; the coordinator retries the page. No labels."),
	); err != nil {
		return fmt.Errorf("register GoFenceAdvisoryLockTimeouts counter: %w", err)
	}
	if inst.GoFenceAdvisoryLockWait, err = meter.Float64Histogram(
		"eshu_dp_go_fence_advisory_lock_wait_seconds",
		metric.WithDescription("Wall time the Go-modules fence waited for the exclusive go-evidence advisory lock, granted or timed out. The tail is the longest supply-chain impact pass write transaction that held the shared lock. No labels."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(goFenceAdvisoryLockWaitBuckets...),
	); err != nil {
		return fmt.Errorf("register GoFenceAdvisoryLockWait histogram: %w", err)
	}
	if inst.SupplyChainImpactWriteTxDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_write_tx_duration_seconds",
		metric.WithDescription("Wall time of one supply-chain impact write transaction, begin to commit or rollback, by outcome: committed, rolled_back (any failure) or recheck_aborted (the go-evidence recheck refused it, so the whole transaction was spent for nothing). The tail is how long a Go-modules fence can wait behind a pass and how much work an abort throws away."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWriteTxBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWriteTxDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactGoEvidenceLockWait, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds",
		metric.WithDescription("Wall time a supply-chain impact pass spent in the closing statements that follow its first shared go-evidence advisory lock: taking the locks, granted or refused, and its owed marker, which can wait on its work-item row. A tail means passes queue behind a Go-modules fence or a stalled marker, and is the time a fence can wait behind the pass. Passes with no resolved Go scope record nothing. No labels."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactGoEvidenceLockWaitBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactGoEvidenceLockWait histogram: %w", err)
	}
	return nil
}

// RecordSupplyChainImpactGoEvidenceRecheckAbort counts one pass aborted at the end of its
// write transaction (its writes rolled back) because its loaded Go evidence went stale (#11327). reason is one of the
// supplychaincore.GoEvidenceCause values; it is a closed set, so no scope or
// generation reaches the metric. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx context.Context, reason string) {
	if i == nil || i.SupplyChainImpactGoEvidenceRecheckAborts == nil {
		return
	}
	i.SupplyChainImpactGoEvidenceRecheckAborts.Add(ctx, 1, metric.WithAttributes(AttrReason(reason)))
}

// RecordGoFenceAdvisoryLockWait records how long the Go-modules fence waited for the
// exclusive go-evidence lock and, when the wait ended in the lock timeout, counts it.
// It carries no label. It is safe on a nil receiver.
func (i *Instruments) RecordGoFenceAdvisoryLockWait(ctx context.Context, wait time.Duration, timedOut bool) {
	if i == nil {
		return
	}
	if i.GoFenceAdvisoryLockWait != nil {
		i.GoFenceAdvisoryLockWait.Record(ctx, wait.Seconds())
	}
	if timedOut && i.GoFenceAdvisoryLockTimeouts != nil {
		i.GoFenceAdvisoryLockTimeouts.Add(ctx, 1)
	}
}

// RecordSupplyChainImpactWriteTx records the wall time of one impact write
// transaction by outcome (committed, rolled_back, recheck_aborted) and, when the pass
// took the shared go-evidence lock, how long taking it waited. It is safe on a nil
// receiver and on an Instruments that did not register the instruments.
func (i *Instruments) RecordSupplyChainImpactWriteTx(ctx context.Context, outcome string, duration time.Duration, lockTaken bool, lockWait time.Duration) {
	if i == nil {
		return
	}
	if i.SupplyChainImpactWriteTxDuration != nil {
		i.SupplyChainImpactWriteTxDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(AttrOutcome(outcome)))
	}
	if lockTaken && i.SupplyChainImpactGoEvidenceLockWait != nil {
		i.SupplyChainImpactGoEvidenceLockWait.Record(ctx, lockWait.Seconds())
	}
}
