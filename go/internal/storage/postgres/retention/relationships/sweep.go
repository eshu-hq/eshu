// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Options configures one pass.
type Options struct {
	// Deletes enables the sweep. False (the default) takes the census and stops:
	// no batch transaction opens and no DELETE is sent.
	Deletes bool
	// RowCap bounds the rows one pass deletes (DefaultRowCap when not
	// positive). The pass checks it between batches; the first batch always
	// runs, so a batch can exceed it.
	RowCap int64
	// Deadline bounds the wall time of one pass; zero means none. The pass
	// checks it between batches; the first batch always runs.
	Deadline time.Time
	// BatchGenerations is the generations per batch (BatchGenerations when not
	// positive). Tests lower it; there is no operator knob.
	BatchGenerations int
	// BatchRows is the row limit of a batch's evidence and candidate deletes
	// (BatchRows when not positive). Tests lower it; there is no operator knob.
	BatchRows int
	// Logger, when non-nil, receives one line per committed batch and one per
	// batch left for a later pass.
	Logger *slog.Logger
	// Tracer, when non-nil, wraps the pass in one span with a span event per
	// committed batch.
	Tracer trace.Tracer

	// afterStatement is the test-only fault hook, called after each batch
	// statement; an error from it aborts the batch like a failed statement.
	afterStatement func(stage string) error
	// now is the test clock; nil uses time.Now.
	now func() time.Time
}

// Result summarizes one pass.
type Result struct {
	// Census is the census the pass started from.
	Census Census
	// RowsSwept is the rows the committed batches deleted, by table. A batch
	// that could not finish a generation (another session held some of its rows)
	// still commits the rows it did delete.
	RowsSwept map[string]int64
	// GenerationsSwept is the generations the committed batches left with no
	// evidence, candidate or generation row, counting the batches' own deletes.
	// A generation another session held rows of is not counted (it is lock_busy
	// and retried), and one whose rows another session or replica removed after
	// the census listed it is counted although this pass deleted none of them.
	GenerationsSwept int
	// Skipped is the generations left for a later pass, by reason (lock_busy,
	// lock_timeout, ineligible). All three reasons always have an entry.
	Skipped map[string]int
	// Batches is the committed batch transactions; a batch that deleted nothing
	// and finished no generation, or rolled back, is not counted.
	Batches int
	// Capped is why the pass stopped with orphans left: CapRows, CapDeadline or
	// CapList. It is empty for a clean pass and for a census-only pass, so a
	// caller that shortens its next wait only after a capped pass never does so
	// after a pass that did nothing.
	Capped string
	// SweepDuration is the batch loop's wall time, zero for a census-only pass.
	SweepDuration time.Duration
}

// sweeper carries one pass's state.
type sweeper struct {
	database       db.Beginner
	afterStatement func(stage string) error
	// batchRows is the row limit of a batch's two picking deletes.
	batchRows int
}

// Sweep takes the orphan census and, when opts.Deletes holds, deletes the
// relationship rows of the generations it listed, in committed batches under a
// row cap and a deadline. It runs outside any retention transaction and holds
// no pass-long transaction or session lock: every batch is its own transaction
// with transaction-scoped per-generation try locks, so a second reducer replica
// skips a generation the first holds and a crash loses at most one uncommitted
// batch.
//
// A batch never waits for a row lock: a generation whose rows another session
// holds (a #10820 purge in flight, a writer) is skipped as lock_busy and the
// rest of the batch commits. A lock timeout (a relation-level lock, such as DDL,
// that the batch cannot skip) skips that batch and the pass continues; any other
// batch error ends the pass with the rows already committed still counted in the
// returned Result. The census always runs, so the orphan gauge stays truthful
// with deletes off.
func Sweep(ctx context.Context, database db.Beginner, opts Options) (Result, error) {
	result := Result{
		RowsSwept: map[string]int64{EvidenceTable: 0, CandidatesTable: 0, GenerationsTable: 0},
		Skipped:   map[string]int{SkipLockBusy: 0, SkipLockTimeout: 0, SkipIneligible: 0},
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}
	ctx, finish := startSpan(ctx, opts.Tracer, opts.Deletes)
	var err error
	defer func() { finish(&result, err) }()

	census, err := TakeCensus(ctx, database)
	if err != nil {
		return result, err
	}
	result.Census = census
	if !opts.Deletes || len(census.Generations) == 0 {
		return result, nil
	}

	size := opts.BatchGenerations
	if size <= 0 {
		size = BatchGenerations
	}
	rowCap := opts.RowCap
	if rowCap <= 0 {
		rowCap = DefaultRowCap
	}
	batchRows := opts.BatchRows
	if batchRows <= 0 {
		batchRows = BatchRows
	}
	s := &sweeper{database: database, afterStatement: opts.afterStatement, batchRows: batchRows}
	started := now()
	var rows int64
	attempted := false
	list := census.Generations
walk:
	for start := 0; start < len(list); start += size {
		batch := list[start:min(start+size, len(list))]
		// A slice is repeated until it is done: a batch whose row limit cut it
		// hands back the generations it left, and they run again before the
		// next slice starts, under the same budget as any other batch.
		for len(batch) > 0 {
			if attempted {
				// The first batch always runs; later batches wait for the budget.
				if rows >= rowCap {
					result.Capped = CapRows
					break walk
				}
				if !opts.Deadline.IsZero() && !now().Before(opts.Deadline) {
					result.Capped = CapDeadline
					break walk
				}
			}
			if err = ctx.Err(); err != nil {
				err = fmt.Errorf("relationship retention sweep: %w", err)
				break walk
			}
			attempted = true
			var outcome batchOutcome
			outcome, err = s.runBatch(ctx, batch)
			if err != nil {
				break walk
			}
			result.Skipped[SkipLockBusy] += outcome.busy
			result.Skipped[SkipIneligible] += outcome.ineligible
			result.Skipped[SkipLockTimeout] += outcome.timedOut
			if outcome.committed {
				for table, n := range outcome.rows {
					result.RowsSwept[table] += n
					rows += n
				}
				result.GenerationsSwept += outcome.swept
				result.Batches++
				logBatch(ctx, opts.Logger, outcome)
				addBatchEvent(ctx, outcome)
			} else {
				logYield(ctx, opts.Logger, outcome, len(batch))
			}
			batch = outcome.continued
		}
	}
	result.SweepDuration = now().Sub(started)
	if err == nil && result.Capped == "" && census.Saturated {
		result.Capped = CapList
	}
	return result, err
}

// startSpan opens the pass span; with no tracer it returns the context and a
// no-op finisher.
func startSpan(ctx context.Context, tracer trace.Tracer, deletes bool) (context.Context, func(*Result, error)) {
	if tracer == nil {
		return ctx, func(*Result, error) {}
	}
	ctx, span := tracer.Start(ctx, telemetry.SpanGenerationRetentionRelationshipSweep)
	span.SetAttributes(attribute.Bool("deletes_enabled", deletes))
	return ctx, func(result *Result, err error) {
		span.SetAttributes(
			attribute.Int("orphan_generations_listed", len(result.Census.Generations)),
			attribute.Bool("census_saturated", result.Census.Saturated),
			attribute.Int("generations_swept", result.GenerationsSwept),
			attribute.Int("batches", result.Batches),
			attribute.String("capped", result.Capped),
		)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}
}

// addBatchEvent records one committed batch on the pass span.
func addBatchEvent(ctx context.Context, outcome batchOutcome) {
	trace.SpanFromContext(ctx).AddEvent(telemetry.GenerationRetentionRelationshipBatchEvent, trace.WithAttributes(
		attribute.Int("generations", outcome.swept),
		attribute.Int64("evidence_rows", outcome.rows[EvidenceTable]),
		attribute.Int64("candidate_rows", outcome.rows[CandidatesTable]),
		attribute.Int64("generation_rows", outcome.rows[GenerationsTable]),
		attribute.Float64("duration_seconds", outcome.duration.Seconds()),
	))
}

// logBatch writes the committed-batch line: counts and time, no identifiers.
// batch_generations is the generations the batch finished and batch_continued the
// ones its row limit cut, which the pass runs again, so a hub generation's chunks
// read as batches that finish none and continue some.
func logBatch(ctx context.Context, logger *slog.Logger, outcome batchOutcome) {
	if logger == nil {
		return
	}
	logger.InfoContext(ctx, "generation retention relationship batch committed",
		slog.Int("batch_generations", outcome.swept),
		slog.Int("batch_continued", len(outcome.continued)),
		slog.Int64("evidence_rows", outcome.rows[EvidenceTable]),
		slog.Int64("candidate_rows", outcome.rows[CandidatesTable]),
		slog.Int64("generation_rows", outcome.rows[GenerationsTable]),
		slog.Int64("batch_rows_deleted", outcome.rows[EvidenceTable]+outcome.rows[CandidatesTable]+outcome.rows[GenerationsTable]),
		slog.Float64("duration_ms", float64(outcome.duration.Microseconds())/1000),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

// logYield writes the line for a batch that committed nothing: every skip reason
// it counted. A lock timeout is a WARN because a table lock held the batch; the
// others are INFO.
func logYield(ctx context.Context, logger *slog.Logger, outcome batchOutcome, size int) {
	if logger == nil {
		return
	}
	level := slog.LevelInfo
	if outcome.timedOut > 0 {
		level = slog.LevelWarn
	}
	logger.Log(ctx, level, "generation retention relationship batch left for a later pass",
		slog.Int("batch_generations", size),
		slog.Int("lock_busy", outcome.busy),
		slog.Int("lock_timeout", outcome.timedOut),
		slog.Int("ineligible", outcome.ineligible),
		slog.Float64("duration_ms", float64(outcome.duration.Microseconds())/1000),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}
