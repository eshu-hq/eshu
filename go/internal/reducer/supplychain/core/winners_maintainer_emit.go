// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The winners maintainer's operator signals (#9774): one span, one duration
// sample, four row-action counts and one info log line per committed resweep, a
// skip count and log line for a resweep that found another in flight, and a
// watermark age the gauge computes at scrape time. None of them carries a
// canonical key, scope, repository or CVE.

// sampleWatermark feeds the committed watermark to the age gauge. It runs before
// the lease claim on every tick, so a statement that never returns still leaves
// the gauge a watermark to age. A failed read keeps the last value, which keeps
// aging: this read is what proves the watermark moved, not what proves it stuck.
func (m SupplyChainImpactWinnersMaintainer) sampleWatermark(ctx context.Context) {
	if m.WatermarkReader == nil || m.Instruments == nil {
		return
	}
	at, ok, err := m.WatermarkReader.Watermark(ctx)
	if err != nil {
		if m.Logger != nil {
			m.Logger.WarnContext(ctx, "supply chain impact winners watermark read failed", "error", err)
		}
		return
	}
	if ok {
		m.Instruments.RecordSupplyChainImpactWinnersWatermark(at)
	}
}

// emitResweepSkipped records a resweep that found another in flight. Skips mean
// a resweep outlasted the lease TTL, so a steady rate is a headroom signal.
func (m SupplyChainImpactWinnersMaintainer) emitResweepSkipped(ctx context.Context, span trace.Span) {
	span.SetAttributes(attribute.Bool("skipped", true))
	if in := m.Instruments; in != nil && in.SupplyChainImpactWinnersResweepSkipped != nil {
		in.SupplyChainImpactWinnersResweepSkipped.Add(ctx, 1)
	}
	if m.Logger != nil {
		m.Logger.InfoContext(ctx, "supply chain impact winners resweep skipped: another resweep is in flight")
	}
}

// startResweepSpan opens the resweep span. A nil tracer returns a no-op span
// so ending it never ends the caller's span.
func (m SupplyChainImpactWinnersMaintainer) startResweepSpan(ctx context.Context) (context.Context, trace.Span) {
	if m.Tracer == nil {
		return ctx, trace.SpanFromContext(context.Background())
	}
	return m.Tracer.Start(ctx, telemetry.SpanSupplyChainImpactWinnersResweep)
}

// emitResweep records every signal of one committed resweep. stamp is the
// watermark the resweep wrote.
func (m SupplyChainImpactWinnersMaintainer) emitResweep(
	ctx context.Context,
	span trace.Span,
	stamp time.Time,
	r SupplyChainImpactWinnersResweep,
	elapsed time.Duration,
) {
	path := r.Path
	if path == "" {
		path = telemetry.SupplyChainImpactWinnersPathFull
	}
	span.SetAttributes(
		attribute.String(telemetry.MetricDimensionPath, path),
		attribute.Int64("keys", r.Keys),
		attribute.Int64("inserted", r.Inserted),
		attribute.Int64("updated", r.Updated),
		attribute.Int64("deleted", r.Deleted),
		attribute.Int64("unchanged", r.Unchanged),
		attribute.Bool("bounded", r.Bounded),
		attribute.Int64("remaining", r.Remaining),
		attribute.Bool("remaining_upper_bound", r.RemainingUpperBound),
		attribute.Int64("chunk_identifiers", r.ChunkIdentifiers),
		attribute.Int64("chunk_kept_rows", r.ChunkKeptRows),
		attribute.Int64("chunk_scanned_copies", r.ChunkScannedCopies),
		attribute.String("chunk_truncated_by", r.ChunkTruncatedBy),
		attribute.String("probe_variant", r.ProbeVariant),
	)
	// watermarkAge is -1 when the table had never been swept: there was no
	// watermark to age.
	watermarkAge := -1.0
	if !r.PreviousWatermark.IsZero() {
		watermarkAge = stamp.Sub(r.PreviousWatermark).Seconds()
	}
	if in := m.Instruments; in != nil {
		if in.SupplyChainImpactWinnersResweepDuration != nil {
			in.SupplyChainImpactWinnersResweepDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(telemetry.AttrPath(path)))
		}
		if in.SupplyChainImpactWinnersRows != nil {
			for action, n := range map[string]int64{
				telemetry.SupplyChainImpactWinnersActionInserted:  r.Inserted,
				telemetry.SupplyChainImpactWinnersActionUpdated:   r.Updated,
				telemetry.SupplyChainImpactWinnersActionDeleted:   r.Deleted,
				telemetry.SupplyChainImpactWinnersActionUnchanged: r.Unchanged,
			} {
				if n > 0 {
					in.SupplyChainImpactWinnersRows.Add(ctx, n, metric.WithAttributes(telemetry.AttrAction(action)))
				}
			}
		}
		m.recordKeyedSignals(ctx, r)
		// Every committed tick stamps (the tick stamp on a drained tick, the
		// horizon over the remainder on a bounded one), so every commit
		// refreshes the gauge with what it stamped (#10706).
		in.RecordSupplyChainImpactWinnersWatermark(stamp)
		in.RecordSupplyChainImpactWinnersCommit(path, time.Now())
	}
	if m.Logger == nil {
		return
	}
	m.Logger.InfoContext(ctx, "supply chain impact winners resweep committed",
		slog.String("path", path),
		slog.Int64("identifiers", r.Identifiers),
		slog.Int64("keys", r.Keys),
		slog.Int64("inserted", r.Inserted),
		slog.Int64("updated", r.Updated),
		slog.Int64("deleted", r.Deleted),
		slog.Int64("unchanged", r.Unchanged),
		slog.Float64("duration_seconds", elapsed.Seconds()),
		slog.Float64("previous_watermark_age_seconds", watermarkAge),
		slog.Bool("bounded", r.Bounded),
		slog.Int64("remaining", r.Remaining),
		slog.Bool("remaining_upper_bound", r.RemainingUpperBound),
		slog.Int64("chunk_identifiers", r.ChunkIdentifiers),
		slog.Int64("chunk_kept_rows", r.ChunkKeptRows),
		slog.Int64("chunk_scanned_copies", r.ChunkScannedCopies),
		slog.String("chunk_truncated_by", r.ChunkTruncatedBy),
		slog.String("probe_variant", r.ProbeVariant),
		slog.Int64("remainder_rows_converted", r.ScopeRemainderConverted),
		slog.Int64("settle_skipped_not_in_chunk", r.SettleSkippedNotInChunk),
		slog.Int64("settle_skipped_version_mismatch", r.SettleSkippedVersionMismatch),
	)
}
