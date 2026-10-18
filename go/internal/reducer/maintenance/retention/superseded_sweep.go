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

// Search index superseded sweep phases on the retention phase histogram
// (#8231). Both run after the prune transaction, outside it.
const (
	phaseListSearchIndexSuperseded  = "census_search_index_superseded"
	phaseSweepSearchIndexSuperseded = "sweep_search_index_superseded"
)

// SupersededSweeper removes the search index rows (terms, documents, stats)
// of superseded generations whose facts the history hold keeps, in bounded
// committed batches (#8231). The storage implementation owns the candidate
// listing, the advisory lock and the budget; deadline bounds the sweep's
// batches and rowCap the rows it deletes (the policy's search index row cap;
// not positive means the storage default).
type SupersededSweeper interface {
	SweepSearchIndexSuperseded(ctx context.Context, deadline time.Duration, rowCap int64) (SupersededSweepResult, error)
}

// SupersededSweepResult summarizes one superseded sweep. RowsSwept is keyed
// by table: eshu_search_index_terms, _documents, and _stats.
type SupersededSweepResult struct {
	Candidates int
	RowsSwept  map[string]int64
	// Batches is the sweep's committed batch transactions; Unswept the listed
	// generations it did not fully sweep (later sweeps retry them).
	Batches       int
	Unswept       int
	ListDuration  time.Duration
	SweepDuration time.Duration
}

// sweepSupersededIfDue runs the superseded sweep at most once per poll
// interval, after the orphan sweep, and only when the policy enables it
// (off by default; enablement stays held until the #8231 gate-(b)
// dead-tuple/bloat evidence lands). Like the orphan sweep it runs after the
// prune transaction has ended, so it never holds or waits on a retention
// scope lock. A failed sweep is recorded and logged but does not fail the
// pass; it is retried after the next interval.
func (r *Runner) sweepSupersededIfDue(ctx context.Context, deadline time.Duration, rowCap int64) {
	if r.SupersededSweeper == nil || !r.Config.Policy.SearchIndexSupersededSweepEnabled || ctx.Err() != nil {
		return
	}
	now := r.now()
	if !r.lastSupersededSweep.IsZero() && now.Sub(r.lastSupersededSweep) < r.Config.pollInterval() {
		return
	}
	r.lastSupersededSweep = now
	result, err := r.SupersededSweeper.SweepSearchIndexSuperseded(ctx, deadline, rowCap)
	if err != nil {
		r.recordSupersededSweepMetrics(ctx, result)
		r.recordSupersededSweepFailure(ctx, err)
		return
	}
	r.recordSupersededSweep(ctx, result)
}

func (r *Runner) recordSupersededSweep(ctx context.Context, result SupersededSweepResult) {
	r.recordSupersededSweepMetrics(ctx, result)
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"generation retention search index superseded sweep completed",
		slog.Int("candidates", result.Candidates),
		slog.Any("rows_swept", result.RowsSwept),
		slog.Int("batches", result.Batches),
		slog.Int("unswept_generations", result.Unswept),
		slog.Float64("list_seconds", result.ListDuration.Seconds()),
		slog.Float64("sweep_seconds", result.SweepDuration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// recordSupersededSweepMetrics records the swept rows and the phase times. A
// failed listing reports no table, so only the phases move.
func (r *Runner) recordSupersededSweepMetrics(ctx context.Context, result SupersededSweepResult) {
	if r.Instruments != nil {
		for table, rows := range result.RowsSwept {
			if rows > 0 {
				r.Instruments.SearchIndexSupersededRowsSwept.Add(ctx, rows, metric.WithAttributes(
					attribute.String("table", table),
				))
			}
		}
		r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.ListDuration.Seconds(), metric.WithAttributes(
			attribute.String("phase", phaseListSearchIndexSuperseded),
		))
		if result.SweepDuration > 0 {
			r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.SweepDuration.Seconds(), metric.WithAttributes(
				attribute.String("phase", phaseSweepSearchIndexSuperseded),
			))
		}
	}
}

func (r *Runner) recordSupersededSweepFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.GenerationRetentionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "search_index_superseded_sweep"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"generation retention search index superseded sweep failed",
			log.Err(err),
			telemetry.FailureClassAttr("generation_retention_search_index_superseded_sweep_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
