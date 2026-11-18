// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DefaultStaleReducerSweepLimit bounds one standalone sweep statement when
// the caller passes no positive limit: at most this many stale rows are
// locked and terminalized per pass, so a deep stale backlog drains over
// several short statements instead of one long lock hold.
const DefaultStaleReducerSweepLimit = 500

// supersedeStaleReducerGenerationsQuery is the standalone periodic sweep
// statement (#10863): the shipped supersede predicate, Crossplane carrier
// arm, and terminal UPDATE (supersedeInactiveReducerGenerationsCTEs),
// with the lock step bounded by LIMIT $3 and made multi-sweeper safe by
// SKIP LOCKED. $1 is the sweep clock (updated_at stamp), $2 the optional
// domain filter (nil sweeps every domain), $3 the row bound. Hand-out
// safety does not depend on this statement — claims fence stale rows
// read-only (reducerClaimStaleGateCTEs) and the runtime generation guard
// refuses a leaked stale intent before its handler runs — so a row this
// pass skips (locked by a claim, a replay, or a sibling sweeper) is
// terminalized by a later pass instead of blocking this one. Lock order
// stays work_item_id COLLATE "C" (#8205), so concurrent sweepers take
// row locks in the same global order and cannot deadlock each other.
var supersedeStaleReducerGenerationsQuery = `
WITH ` + reducerClaimReadinessRequirementsCTE() + `,
` + supersedeInactiveReducerGenerationsCTEs(`
    ORDER BY stale.work_item_id COLLATE "C"
    LIMIT $3
    FOR NO KEY UPDATE OF stale SKIP LOCKED
`) + `
SELECT count(*) FROM superseded_stale_reducer_generations
`

// SupersedeStaleReducerGenerations terminalizes up to limit unleased
// older-generation reducer rows in one bounded statement and returns how
// many rows it superseded. It is the out-of-claim home of the cleanup the
// reducer claims used to run inline (#10863); the claims themselves only
// fence stale rows at hand-out now. A domain filter on the queue
// (ClaimDomain/ClaimDomains) scopes the sweep to the same domains the
// queue may claim; the shared sweeper queues carry no filter and sweep
// every domain. limit <= 0 uses DefaultStaleReducerSweepLimit.
func (q ReducerQueue) SupersedeStaleReducerGenerations(ctx context.Context, limit int) (int, error) {
	if q.database == nil {
		return 0, errors.New("reducer stale-generation sweep requires a database")
	}
	if limit <= 0 {
		limit = DefaultStaleReducerSweepLimit
	}
	rows, err := q.database.QueryContext(ctx, supersedeStaleReducerGenerationsQuery, q.now(), q.claimDomainFilters(), limit)
	if err != nil {
		return 0, fmt.Errorf("supersede stale reducer generations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("supersede stale reducer generations: %w", err)
		}
		return 0, errors.New("supersede stale reducer generations returned no count row")
	}
	var swept int64
	if err := rows.Scan(&swept); err != nil {
		return 0, fmt.Errorf("supersede stale reducer generations: scan count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("supersede stale reducer generations: %w", err)
	}
	return int(swept), nil
}

// ReducerStaleGenerationSweeper runs SupersedeStaleReducerGenerations on
// an interval (#10863). The sweep is idempotent and SKIP LOCKED bounded,
// so every reducer replica may run one without a lease: concurrent
// sweepers partition the stale backlog by whichever rows each locks
// first, and a row none of them reaches in one cycle is terminalized in
// the next. Claims never wait for it — they fence stale rows read-only.
type ReducerStaleGenerationSweeper struct {
	// Queue supplies the database, clock, domain filter, and instruments.
	Queue ReducerQueue
	// Interval is the delay between cycles; zero means 30 seconds.
	Interval time.Duration
	// BatchSize bounds one sweep statement; zero means
	// DefaultStaleReducerSweepLimit.
	BatchSize int
	// MaxPassesPerCycle bounds how many bounded statements one cycle may
	// chain while passes keep returning a full batch; zero means 10. The
	// bound keeps one cycle from monopolizing a connection draining a
	// very deep stale backlog; the rest waits for the next cycle.
	MaxPassesPerCycle int
	// Logger receives one line per cycle that superseded rows, plus
	// failures. Nil disables logging.
	Logger *slog.Logger
	// Now and Wait are test seams; nil means time.Now and a timer.
	Now  func() time.Time
	Wait func(context.Context, time.Duration) error
}

// Run sweeps once at startup, then once per interval until ctx ends.
func (s ReducerStaleGenerationSweeper) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.SweepOnce(ctx)
		if err := s.wait(ctx, s.interval()); err != nil {
			return err
		}
	}
}

// SweepOnce runs bounded sweep passes until one pass supersedes fewer
// rows than the batch bound (backlog drained for now) or the per-cycle
// pass bound is reached, and records the cycle's rows, duration, and
// outcome. A failed pass is logged and counted, never returned: the next
// cycle retries, and claims stay correct through the gate meanwhile.
func (s ReducerStaleGenerationSweeper) SweepOnce(ctx context.Context) {
	started := s.clock()
	total := 0
	passes := 0
	var sweepErr error
	for passes < s.maxPasses() {
		n, err := s.Queue.SupersedeStaleReducerGenerations(ctx, s.batchSize())
		if err != nil {
			sweepErr = err
			break
		}
		passes++
		total += n
		if n < s.batchSize() {
			break
		}
	}
	duration := s.clock().Sub(started)
	if ctx.Err() != nil {
		return
	}
	outcome := "ok"
	if sweepErr != nil {
		outcome = "failed"
	}
	if inst := s.Queue.Instruments; inst != nil {
		attrs := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, outcome))
		if inst.ReducerStaleSweepPasses != nil {
			inst.ReducerStaleSweepPasses.Add(ctx, 1, attrs)
		}
		if inst.ReducerStaleSweepDuration != nil {
			inst.ReducerStaleSweepDuration.Record(ctx, duration.Seconds(), attrs)
		}
		if sweepErr == nil && total > 0 && inst.ReducerStaleSweptRows != nil {
			inst.ReducerStaleSweptRows.Add(ctx, int64(total))
		}
	}
	if s.Logger == nil {
		return
	}
	if sweepErr != nil {
		s.Logger.Error("reducer stale-generation sweep failed",
			slog.String("error", sweepErr.Error()),
			slog.Int("rows_superseded_before_failure", total),
			slog.Int("passes", passes),
			slog.Float64("duration_seconds", duration.Seconds()),
			telemetry.FailureClassAttr("reducer_stale_sweep_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction))
		return
	}
	if total > 0 {
		s.Logger.Info("reducer stale-generation sweep",
			slog.Int("rows_superseded", total),
			slog.Int("passes", passes),
			slog.Float64("duration_seconds", duration.Seconds()),
			telemetry.PhaseAttr(telemetry.PhaseReduction))
	}
}

func (s ReducerStaleGenerationSweeper) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return 30 * time.Second
}

func (s ReducerStaleGenerationSweeper) batchSize() int {
	if s.BatchSize > 0 {
		return s.BatchSize
	}
	return DefaultStaleReducerSweepLimit
}

func (s ReducerStaleGenerationSweeper) maxPasses() int {
	if s.MaxPassesPerCycle > 0 {
		return s.MaxPassesPerCycle
	}
	return 10
}

func (s ReducerStaleGenerationSweeper) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s ReducerStaleGenerationSweeper) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
