// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	deferredMaintenanceBarrierName         = "ingester_deferred_relationship_maintenance"
	deferredMaintenanceBarrierStateLockKey = 0x455348554d4253
)

const selectLatestDeferredMaintenanceBarrierSQL = `
SELECT epoch, shard_count, completed_at
FROM deferred_maintenance_barriers
WHERE barrier_name = $1
ORDER BY epoch DESC
LIMIT 1
FOR UPDATE
`

const insertDeferredMaintenanceBarrierSQL = `
INSERT INTO deferred_maintenance_barriers (
    barrier_name, epoch, shard_count, created_at, updated_at
) VALUES ($1, $2, $3, $4, $4)
`

const recordDeferredMaintenanceBarrierArrivalSQL = `
INSERT INTO deferred_maintenance_barrier_arrivals (
    barrier_name, epoch, shard_index, arrived_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (barrier_name, epoch, shard_index) DO UPDATE
SET arrived_at = EXCLUDED.arrived_at
`

const countDeferredMaintenanceBarrierArrivalsSQL = `
SELECT COUNT(*)
FROM deferred_maintenance_barrier_arrivals
WHERE barrier_name = $1
  AND epoch = $2
`

const completeDeferredMaintenanceBarrierSQL = `
UPDATE deferred_maintenance_barriers
SET leader_shard_index = $3,
    completed_at = $4,
    updated_at = $4
WHERE barrier_name = $1
  AND epoch = $2
`

// DeferredMaintenanceBarrierConfig identifies one sharded ingester's
// participation in the fleet-wide deferred-maintenance barrier.
type DeferredMaintenanceBarrierConfig struct {
	ShardCount int
	ShardIndex int
	// HasCommitted reports whether this shard actually committed a generation
	// since its last barrier drain — the collector passes the same value it
	// used to decide committedSinceDrain for this cycle (see
	// collector.Service.Run). It gates a join-only invariant: a shard that has
	// not committed anything may still join an epoch that is already open (that
	// is what unblocks a fleet where one shard owns no repositories — #5852),
	// but it must never be the one that OPENS a new epoch. Opening is reserved
	// for shards that actually have committed work to account for; otherwise a
	// quiet fleet where nothing ever commits would keep opening and completing
	// empty epochs forever, running the corpus-wide maintenance pass against an
	// unchanged corpus on every idle poll.
	HasCommitted bool
}

func (c DeferredMaintenanceBarrierConfig) validate() error {
	if c.ShardCount < 1 {
		return fmt.Errorf("deferred maintenance shard count must be positive")
	}
	if c.ShardIndex < 0 {
		return fmt.Errorf("deferred maintenance shard index must be non-negative")
	}
	if c.ShardIndex >= c.ShardCount {
		return fmt.Errorf("deferred maintenance shard index %d must be less than shard count %d", c.ShardIndex, c.ShardCount)
	}
	return nil
}

// RunDeferredRelationshipMaintenanceAfterShardDrain records this shard's drain
// arrival and runs global deferred maintenance only after every shard in the
// current epoch has arrived.
//
// Single-shard runtimes ARE the entire fleet, so the same join-only invariant
// collapses to a direct check: a single shard that has not committed anything
// since its last drain has nothing to account for and skips maintenance
// outright, rather than running it unconditionally on every idle poll. A
// single shard that has committed runs maintenance directly, matching the
// original single-shard behavior.
func (s IngestionStore) RunDeferredRelationshipMaintenanceAfterShardDrain(
	ctx context.Context,
	config DeferredMaintenanceBarrierConfig,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	if err := config.validate(); err != nil {
		return err
	}
	if config.ShardCount == 1 {
		if !config.HasCommitted {
			return nil
		}
		return s.RunDeferredRelationshipMaintenance(ctx, tracer, instruments)
	}
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required")
	}

	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deferred maintenance barrier transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceStateBarrier(ctx, tx); err != nil {
		return fmt.Errorf("acquire deferred maintenance state barrier: %w", err)
	}
	now := s.now()
	epoch, hasEpoch, err := ensureDeferredMaintenanceBarrierEpoch(ctx, tx, config.ShardCount, now, config.HasCommitted)
	if err != nil {
		return err
	}
	if !hasEpoch {
		// No epoch is open and this shard has not committed anything since its
		// last drain: there is nothing to join and this shard must not be the
		// one that opens a new epoch (see DeferredMaintenanceBarrierConfig.
		// HasCommitted). Release the barrier state lock and return without
		// recording an arrival or waiting — a quiet fleet stays quiet.
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit deferred maintenance barrier (no epoch to join): %w", err)
		}
		committed = true
		// Rate-limited: AfterEmptyBatchDrained re-checks this on every idle
		// poll for as long as this shard never commits (see
		// startupMaintenanceEscapeUsed in collector.Service.Run), and the
		// ingester's poll interval is 1s — unthrottled this is one identical
		// INFO line per second forever. idleMaintenanceLogGate caps it at one
		// line per deferredMaintenanceBarrierStallLogInterval.
		if s.Logger != nil && s.idleMaintenanceLogGate.shouldLog(now) {
			s.Logger.InfoContext(
				ctx, "deferred maintenance barrier idle; no epoch to join",
				telemetry.PhaseAttr("deferred_maintenance_barrier"),
				"shard_count", config.ShardCount,
				"shard_index", config.ShardIndex,
			)
		}
		return nil
	}
	arrivedCount, err := recordDeferredMaintenanceBarrierArrival(ctx, tx, epoch, config.ShardIndex, now)
	if err != nil {
		return err
	}
	if arrivedCount < config.ShardCount {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit deferred maintenance barrier arrival: %w", err)
		}
		committed = true
		if s.Logger != nil {
			s.Logger.InfoContext(
				ctx, "deferred maintenance barrier waiting for shards",
				telemetry.PhaseAttr("deferred_maintenance_barrier"),
				"epoch", epoch,
				"arrived_shards", arrivedCount,
				"shard_count", config.ShardCount,
				"shard_index", config.ShardIndex,
			)
		}
		return s.waitDeferredMaintenanceBarrierCompletion(ctx, epoch, config)
	}

	// Commit the leader's arrival and release the barrier state lock before
	// running maintenance. Maintenance then runs in its own bounded per-repository
	// batch transactions (see BackfillAllRelationshipEvidence), so the leader
	// never holds the barrier state lock or a fleet-wide maintenance lock during
	// the corpus-wide pass. Non-leader shards keep polling for completion, which
	// is marked only after maintenance succeeds.
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deferred maintenance barrier arrival: %w", err)
	}
	committed = true

	if err := s.RunDeferredRelationshipMaintenance(ctx, tracer, instruments); err != nil {
		return err
	}

	if err := s.markDeferredMaintenanceBarrierComplete(ctx, epoch, config.ShardIndex); err != nil {
		return err
	}
	if s.Logger != nil {
		s.Logger.InfoContext(
			ctx, "deferred maintenance barrier completed",
			telemetry.PhaseAttr("deferred_maintenance_barrier"),
			"epoch", epoch,
			"arrived_shards", arrivedCount,
			"shard_count", config.ShardCount,
			"leader_shard_index", config.ShardIndex,
		)
	}
	return nil
}

// markDeferredMaintenanceBarrierComplete records barrier completion for the
// epoch in its own short transaction after the leader's maintenance pass
// finishes. Waiting shards poll for this row, so it must be written only after
// maintenance has committed its per-batch work.
func (s IngestionStore) markDeferredMaintenanceBarrierComplete(
	ctx context.Context,
	epoch int64,
	leaderShardIndex int,
) error {
	if s.beginner == nil {
		return fmt.Errorf("transaction beginner is required")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deferred maintenance completion transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := completeDeferredMaintenanceBarrier(ctx, tx, epoch, leaderShardIndex, s.now()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deferred maintenance completion: %w", err)
	}
	committed = true
	return nil
}

// waitDeferredMaintenanceBarrierCompletion (deferred_maintenance_barrier_stall.go)
// blocks a non-leader shard until the leader marks epoch complete, logging a
// stall warning — naming arrived and missing shard indexes — if the wait runs
// long. See that file's doc comment for the unbounded-wait rationale.

func acquireDeferredMaintenanceStateBarrier(ctx context.Context, db ExecQueryer) error {
	_, err := db.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", deferredMaintenanceBarrierStateLockKey)
	return err
}

// ensureDeferredMaintenanceBarrierEpoch resolves the epoch this shard should
// join, or reports that there is none to join. When an epoch is already open
// (not yet completed), every shard may join it regardless of canOpenEpoch —
// that is the #5852 stall fix: a never-committed shard must still be able to
// unblock a fleet another shard already started draining. When no epoch is
// open, only a shard that has actual committed work to account for
// (canOpenEpoch true) may open a new one; a shard with nothing to report gets
// hasEpoch false and must not create one. This is the join-only half of the
// design: opening is reserved for shards with real work, joining is open to
// everyone, so a quiet fleet where nothing ever commits never opens an epoch
// at all (see DeferredMaintenanceBarrierConfig.HasCommitted).
func ensureDeferredMaintenanceBarrierEpoch(
	ctx context.Context,
	tx Transaction,
	shardCount int,
	now time.Time,
	canOpenEpoch bool,
) (epoch int64, hasEpoch bool, err error) {
	rows, err := tx.QueryContext(ctx, selectLatestDeferredMaintenanceBarrierSQL, deferredMaintenanceBarrierName)
	if err != nil {
		return 0, false, fmt.Errorf("query deferred maintenance barrier: %w", err)
	}

	var latestEpoch int64
	var latestShardCount int
	var completedAt sql.NullTime
	var scanErr error
	if rows.Next() {
		if err := rows.Scan(&latestEpoch, &latestShardCount, &completedAt); err != nil {
			scanErr = fmt.Errorf("scan deferred maintenance barrier: %w", err)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, false, fmt.Errorf("close deferred maintenance barrier rows: %w", err)
	}
	if scanErr != nil {
		return 0, false, scanErr
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("scan deferred maintenance barrier rows: %w", err)
	}

	if latestEpoch > 0 && !completedAt.Valid && latestShardCount != shardCount {
		return 0, false, fmt.Errorf("deferred maintenance barrier epoch %d is open with shard count %d, refusing shard count %d", latestEpoch, latestShardCount, shardCount)
	}
	if latestEpoch > 0 && !completedAt.Valid {
		return latestEpoch, true, nil
	}
	if !canOpenEpoch {
		return 0, false, nil
	}
	nextEpoch := latestEpoch + 1
	if _, err := tx.ExecContext(
		ctx,
		insertDeferredMaintenanceBarrierSQL,
		deferredMaintenanceBarrierName,
		nextEpoch,
		shardCount,
		now,
	); err != nil {
		return 0, false, fmt.Errorf("insert deferred maintenance barrier epoch: %w", err)
	}
	return nextEpoch, true, nil
}

func recordDeferredMaintenanceBarrierArrival(
	ctx context.Context,
	tx Transaction,
	epoch int64,
	shardIndex int,
	now time.Time,
) (int, error) {
	if _, err := tx.ExecContext(
		ctx,
		recordDeferredMaintenanceBarrierArrivalSQL,
		deferredMaintenanceBarrierName,
		epoch,
		shardIndex,
		now,
	); err != nil {
		return 0, fmt.Errorf("record deferred maintenance barrier arrival: %w", err)
	}
	rows, err := tx.QueryContext(ctx, countDeferredMaintenanceBarrierArrivalsSQL, deferredMaintenanceBarrierName, epoch)
	if err != nil {
		return 0, fmt.Errorf("count deferred maintenance barrier arrivals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, fmt.Errorf("count deferred maintenance barrier arrivals: no rows")
	}
	var arrivedCount int
	if err := rows.Scan(&arrivedCount); err != nil {
		return 0, fmt.Errorf("scan deferred maintenance barrier arrival count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("scan deferred maintenance barrier arrival rows: %w", err)
	}
	return arrivedCount, nil
}

// deferredMaintenanceBarrierCompleted, deferredMaintenanceBarrierArrivedShardIndexes,
// and missingDeferredMaintenanceBarrierShardIndexes live in
// deferred_maintenance_barrier_stall.go, alongside the stall-watchdog wait
// loop that is their only caller.

func completeDeferredMaintenanceBarrier(
	ctx context.Context,
	tx Transaction,
	epoch int64,
	shardIndex int,
	now time.Time,
) error {
	_, err := tx.ExecContext(
		ctx,
		completeDeferredMaintenanceBarrierSQL,
		deferredMaintenanceBarrierName,
		epoch,
		shardIndex,
		now,
	)
	if err != nil {
		return fmt.Errorf("complete deferred maintenance barrier: %w", err)
	}
	return nil
}
