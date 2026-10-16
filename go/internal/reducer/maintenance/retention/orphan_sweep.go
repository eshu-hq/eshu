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

// Search index orphan sweep phases on the retention phase histogram (#7427
// PR2). Both run after the prune transaction, outside it.
const (
	phaseCensusSearchIndexOrphans = "census_search_index_orphans"
	phaseSweepSearchIndexOrphans  = "sweep_search_index_orphans"
)

// OrphanSweeper takes a census of search index rows whose scope_generations
// row is gone and deletes them in bounded committed batches (#7427 PR2). The
// storage implementation owns the SQL, the advisory lock and the budget;
// deadline bounds the sweep's batches and rowCap the rows it deletes (the
// policy's search index row cap; not positive means the storage default).
type OrphanSweeper interface {
	SweepSearchIndexOrphans(ctx context.Context, deadline time.Duration, rowCap int64) (OrphanSweepResult, error)
}

// OrphanSweepResult summarizes one orphan census and sweep. Every map is
// keyed by table: eshu_search_index_terms and eshu_search_index_documents.
type OrphanSweepResult struct {
	PairsScanned      map[string]int64
	OrphanGenerations map[string]int64
	RowsSwept         map[string]int64
	// Batches is the sweep's committed batch transactions; Unswept the orphan
	// generations it did not sweep, listed or past the census list limit
	// (later sweeps retry them).
	Batches        int
	Unswept        int
	CensusDuration time.Duration
	SweepDuration  time.Duration
}

// sweepOrphansIfDue runs the orphan sweep at most once per poll interval: a
// draining backlog runs retention passes back to back, and the census walks
// every search index pair, so it follows the poll cadence, not the pass
// rate. A failed sweep is recorded and logged but does not fail the pass; it
// is retried after the next interval.
func (r *Runner) sweepOrphansIfDue(ctx context.Context, deadline time.Duration, rowCap int64) {
	if r.OrphanSweeper == nil || ctx.Err() != nil {
		return
	}
	now := r.now()
	if !r.lastOrphanSweep.IsZero() && now.Sub(r.lastOrphanSweep) < r.Config.pollInterval() {
		return
	}
	r.lastOrphanSweep = now
	result, err := r.OrphanSweeper.SweepSearchIndexOrphans(ctx, deadline, rowCap)
	if err != nil {
		// A census that succeeded before a batch failed still sets the
		// gauge, so it never keeps a stale value (such as 0) while orphans
		// exist, and rows the committed batches swept stay counted. A failed
		// census reports no table and leaves the gauge as it was.
		r.recordOrphanSweepMetrics(ctx, result)
		r.recordOrphanSweepFailure(ctx, err)
		return
	}
	r.recordOrphanSweep(ctx, result)
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) recordOrphanSweep(ctx context.Context, result OrphanSweepResult) {
	r.recordOrphanSweepMetrics(ctx, result)
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"generation retention search index orphan sweep completed",
		slog.Any("pairs_scanned", result.PairsScanned),
		slog.Any("orphan_generations", result.OrphanGenerations),
		slog.Any("rows_swept", result.RowsSwept),
		slog.Int("batches", result.Batches),
		slog.Int("unswept_generations", result.Unswept),
		slog.Float64("census_seconds", result.CensusDuration.Seconds()),
		slog.Float64("sweep_seconds", result.SweepDuration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// recordOrphanSweepMetrics records the census gauge for each table the result
// reports, the swept rows and the phase times. A failed census reports no
// table, so the gauge keeps its last census value.
func (r *Runner) recordOrphanSweepMetrics(ctx context.Context, result OrphanSweepResult) {
	if r.Instruments != nil {
		for table, orphans := range result.OrphanGenerations {
			r.Instruments.SearchIndexOrphanGenerations.Record(ctx, orphans, metric.WithAttributes(
				attribute.String("table", table),
			))
		}
		for table, rows := range result.RowsSwept {
			if rows > 0 {
				r.Instruments.SearchIndexOrphanRowsSwept.Add(ctx, rows, metric.WithAttributes(
					attribute.String("table", table),
				))
			}
		}
		r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.CensusDuration.Seconds(), metric.WithAttributes(
			attribute.String("phase", phaseCensusSearchIndexOrphans),
		))
		if result.SweepDuration > 0 {
			r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, result.SweepDuration.Seconds(), metric.WithAttributes(
				attribute.String("phase", phaseSweepSearchIndexOrphans),
			))
		}
	}
}

func (r *Runner) recordOrphanSweepFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.GenerationRetentionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "search_index_orphan_sweep"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"generation retention search index orphan sweep failed",
			log.Err(err),
			telemetry.FailureClassAttr("generation_retention_search_index_orphan_sweep_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
