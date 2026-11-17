// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Relationship retention phases on the retention phase histogram (#10823). Both
// run after the prune transaction, outside it.
const (
	phaseCensusRelationshipOrphans = "census_relationship_orphans"
	phaseSweepRelationshipOrphans  = "sweep_relationship_orphans"
)

// relationshipAcceleratedDivisor shortens the wait before the next sweep to a
// sixth of the poll interval after a capped pass, so a large backlog drains in
// hours instead of days while a quiet system keeps the poll cadence.
const relationshipAcceleratedDivisor = 6

// RelationshipSkipLockTimeout is the Skipped key of a batch a lock timeout
// ended (a concurrent purge held its rows). A capped pass that also hit one is
// contended, not behind, so it keeps the poll interval. The storage leaf owns
// the spelling; the reducer wiring pins the two equal.
const RelationshipSkipLockTimeout = "lock_timeout"

// RelationshipSweeper takes the relationship retention census and, when the
// request enables deletes, removes the evidence, candidate and generation rows
// of generations nothing keeps alive, in bounded committed batches (#10823).
// The storage implementation owns the SQL, the locks and the batch loop;
// Deadline bounds the batches' wall time and RowCap the rows they delete (not
// positive means the storage default). With Deletes false it takes the
// read-only census and nothing else.
type RelationshipSweeper interface {
	SweepRelationships(ctx context.Context, request RelationshipSweepRequest) (RelationshipSweepResult, error)
}

// RelationshipSweepRequest is one pass's bounds.
type RelationshipSweepRequest struct {
	Deadline time.Duration
	RowCap   int64
	Deletes  bool
}

// RelationshipSweepResult summarizes one pass. The maps are keyed by table
// (relationship_evidence_facts, relationship_candidates,
// relationship_generations) except Skipped, which is keyed by reason
// (lock_busy, lock_timeout, ineligible).
type RelationshipSweepResult struct {
	// OrphanGenerations is the distinct orphan generations the census found per
	// table; it saturates at the census list limit (CensusSaturated).
	OrphanGenerations map[string]int64
	CensusSaturated   bool
	RowsSwept         map[string]int64
	GenerationsSwept  int
	Skipped           map[string]int
	// Batches is the committed batch transactions.
	Batches int
	// Capped is why the pass stopped with orphans left: rows, deadline or list.
	// It is empty for a clean or census-only pass.
	Capped         string
	CensusDuration time.Duration
	SweepDuration  time.Duration
}

// sweepRelationshipsIfDue runs the relationship sweep after the retention
// pass, outside its transaction. The census (and the orphan gauge) runs
// whenever the sweeper is wired; deletes run only when the policy enables
// them. It runs at most once per poll interval, or once per sixth of it after
// a capped pass, so a draining backlog is not throttled to the poll cadence
// while a clean, failing or contended system is. A failed sweep is recorded and
// logged but does not fail the pass.
func (r *Runner) sweepRelationshipsIfDue(ctx context.Context, policy Policy) {
	if r.RelationshipSweeper == nil || ctx.Err() != nil {
		return
	}
	now := r.now()
	if !r.lastRelationshipSweep.IsZero() && now.Sub(r.lastRelationshipSweep) < r.relationshipInterval() {
		return
	}
	r.lastRelationshipSweep = now
	r.relationshipCapped = false
	result, err := r.RelationshipSweeper.SweepRelationships(ctx, RelationshipSweepRequest{
		Deadline: r.Config.pollInterval() / generationRetentionPredeleteDeadlineDivisor,
		RowCap:   policy.RelationshipRetentionRowCap,
		Deletes:  policy.RelationshipRetentionSweepEnabled,
	})
	if err != nil {
		// A census that succeeded before a batch failed still sets the gauge and
		// counts the committed rows. A failed pass never shortens the wait.
		r.recordRelationshipSweepMetrics(ctx, result)
		r.recordRelationshipSweepFailure(ctx, err)
		return
	}
	// A pass that hit a lock timeout is contended, not behind: it keeps the
	// poll interval so it never hammers a purge or an activation.
	r.relationshipCapped = result.Capped != "" && result.Skipped[RelationshipSkipLockTimeout] == 0
	r.recordRelationshipSweep(ctx, result, policy.RelationshipRetentionSweepEnabled)
}

// relationshipInterval is the minimum gap between two sweeps: the poll
// interval, or a sixth of it after a capped pass.
func (r *Runner) relationshipInterval() time.Duration {
	if r.relationshipCapped {
		return r.Config.pollInterval() / relationshipAcceleratedDivisor
	}
	return r.Config.pollInterval()
}

// shortenForRelationshipBacklog returns the wait the loop should use: wait, or
// a sixth of the poll interval when the last relationship pass was capped and
// that is shorter.
func (r *Runner) shortenForRelationshipBacklog(wait time.Duration) time.Duration {
	if !r.relationshipCapped {
		return wait
	}
	return min(wait, r.Config.pollInterval()/relationshipAcceleratedDivisor)
}

func (r *Runner) recordRelationshipSweep(ctx context.Context, result RelationshipSweepResult, deletes bool) {
	r.recordRelationshipSweepMetrics(ctx, result)
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"generation retention relationship sweep completed",
		slog.Bool("deletes_enabled", deletes),
		slog.Any("orphan_generations", result.OrphanGenerations),
		slog.Bool("census_saturated", result.CensusSaturated),
		slog.Any("rows_swept", result.RowsSwept),
		slog.Int("generations_swept", result.GenerationsSwept),
		slog.Any("skipped", result.Skipped),
		slog.Int("batches", result.Batches),
		slog.String("capped", result.Capped),
		slog.Float64("census_seconds", result.CensusDuration.Seconds()),
		slog.Float64("sweep_seconds", result.SweepDuration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// recordRelationshipSweepMetrics records the census gauge for each table the
// result reports, the swept rows, the skipped generations, the cap and the
// phase times. A failed census reports no table, so the gauge keeps its last
// value.
func (r *Runner) recordRelationshipSweepMetrics(ctx context.Context, result RelationshipSweepResult) {
	if r.Instruments == nil {
		return
	}
	for table, orphans := range result.OrphanGenerations {
		r.Instruments.RelationshipRetentionOrphanGenerations.Record(ctx, orphans, metric.WithAttributes(
			attribute.String("table", table),
		))
	}
	for table, rows := range result.RowsSwept {
		if rows > 0 {
			r.Instruments.RelationshipRetentionRowsSwept.Add(ctx, rows, metric.WithAttributes(
				attribute.String("table", table),
			))
		}
	}
	for reason, generations := range result.Skipped {
		if generations > 0 {
			r.Instruments.RelationshipRetentionGenerationsSkipped.Add(ctx, int64(generations), metric.WithAttributes(
				attribute.String("reason", reason),
			))
		}
	}
	if result.Capped != "" {
		r.Instruments.RelationshipRetentionCapped.Add(ctx, 1, metric.WithAttributes(
			attribute.String("cap", result.Capped),
		))
	}
	if result.CensusDuration > 0 {
		r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.CensusDuration.Seconds(), metric.WithAttributes(
			attribute.String("phase", phaseCensusRelationshipOrphans),
		))
	}
	if result.SweepDuration > 0 {
		r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.SweepDuration.Seconds(), metric.WithAttributes(
			attribute.String("phase", phaseSweepRelationshipOrphans),
		))
	}
}

func (r *Runner) recordRelationshipSweepFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.GenerationRetentionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "relationship_orphan_sweep"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"generation retention relationship sweep failed",
			log.Err(err),
			telemetry.FailureClassAttr("generation_retention_relationship_sweep_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
