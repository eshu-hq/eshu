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

// phaseCensusSearchIndexPartitionSizes is the per-partition size-vs-rowcount
// census phase on the retention phase histogram (#10874).
const phaseCensusSearchIndexPartitionSizes = "census_search_index_partition_sizes"

// Partition size suspect classes, as the storage census classifies them
// (#10874). Empty means the leaf is clear.
const (
	partitionSizeSuspectWarn = "warn"
	partitionSizeSuspectPage = "page"
)

// PartitionLeafSize is one terms leaf's size-vs-rowcount row. Partition is
// the closed p00-p63 label set; scope and generation identifiers never reach
// metric labels.
type PartitionLeafSize struct {
	Partition     string
	Leaf          string
	TotalBytes    int64
	HeapBytes     int64
	IndexBytes    int64
	EstimatedRows int64
	BytesPerRow   float64
	// Suspect is "", "warn", or "page".
	Suspect string
}

// PartitionSizesResult is one per-partition size census.
type PartitionSizesResult struct {
	Leaves         []PartitionLeafSize
	CensusDuration time.Duration
}

// PartitionSizesCensuser takes a catalog-only per-partition size census of
// the search index terms leaves (#10874). The storage implementation owns
// the SQL and the WARN/PAGE classification.
type PartitionSizesCensuser interface {
	CensusSearchIndexPartitionSizes(context.Context) (PartitionSizesResult, error)
}

// censusPartitionSizesIfDue runs the per-partition size census on the
// orphan-sweep cadence: the caller sets lastOrphanSweep, so a draining
// backlog running passes back to back still censuses at most once per poll
// interval. A failed census is recorded and logged but does not fail the
// pass; it is retried after the next interval.
func (r *Runner) censusPartitionSizesIfDue(ctx context.Context, due bool) {
	if r.PartitionSizes == nil || !due || ctx.Err() != nil {
		return
	}
	started := time.Now()
	result, err := r.PartitionSizes.CensusSearchIndexPartitionSizes(ctx)
	if err != nil {
		r.recordPartitionSizesFailure(ctx, err)
		return
	}
	r.recordPartitionSizes(ctx, result, time.Since(started))
}

func (r *Runner) recordPartitionSizes(ctx context.Context, result PartitionSizesResult, duration time.Duration) {
	censusSeconds := result.CensusDuration.Seconds()
	if censusSeconds <= 0 {
		censusSeconds = duration.Seconds()
	}
	if r.Instruments != nil {
		for _, leaf := range result.Leaves {
			r.Instruments.SearchIndexPartitionBytes.Record(ctx, leaf.TotalBytes, metric.WithAttributes(
				attribute.String("partition", leaf.Partition),
				attribute.String("component", "total"),
			))
			r.Instruments.SearchIndexPartitionBytes.Record(ctx, leaf.HeapBytes, metric.WithAttributes(
				attribute.String("partition", leaf.Partition),
				attribute.String("component", "heap"),
			))
			r.Instruments.SearchIndexPartitionBytes.Record(ctx, leaf.IndexBytes, metric.WithAttributes(
				attribute.String("partition", leaf.Partition),
				attribute.String("component", "index"),
			))
			r.Instruments.SearchIndexPartitionEstimatedRows.Record(ctx, leaf.EstimatedRows, metric.WithAttributes(
				attribute.String("partition", leaf.Partition),
			))
			r.Instruments.SearchIndexPartitionBytesPerRow.Record(ctx, leaf.BytesPerRow, metric.WithAttributes(
				attribute.String("partition", leaf.Partition),
				attribute.String("component", "total"),
			))
		}
		r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, censusSeconds, metric.WithAttributes(
			attribute.String("phase", phaseCensusSearchIndexPartitionSizes),
		))
	}
	if r.Logger == nil {
		return
	}
	for _, leaf := range result.Leaves {
		switch leaf.Suspect {
		case partitionSizeSuspectWarn, partitionSizeSuspectPage:
		default:
			continue
		}
		r.Logger.WarnContext(
			ctx,
			"generation retention search index partition size suspect",
			slog.String("partition", leaf.Partition),
			slog.String("leaf", leaf.Leaf),
			slog.String("suspect", leaf.Suspect),
			slog.Int64("total_bytes", leaf.TotalBytes),
			slog.Int64("heap_bytes", leaf.HeapBytes),
			slog.Int64("index_bytes", leaf.IndexBytes),
			slog.Int64("estimated_rows", leaf.EstimatedRows),
			slog.Float64("bytes_per_row", leaf.BytesPerRow),
			slog.Float64("census_seconds", censusSeconds),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func (r *Runner) recordPartitionSizesFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.GenerationRetentionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "search_index_partition_size_census"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"generation retention search index partition size census failed",
			log.Err(err),
			telemetry.FailureClassAttr("generation_retention_search_index_partition_size_census_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
