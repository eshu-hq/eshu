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
	deferredMaintenanceBarrierPollInterval = 250 * time.Millisecond
	// deferredMaintenanceBarrierStallLogInterval bounds how often a shard
	// stuck in waitDeferredMaintenanceBarrierCompletion re-announces the
	// stall. The wait itself stays unbounded (see the function doc for why),
	// but a shard that has been waiting this long or longer logs a WARN with
	// the current arrival count on every subsequent poll tick until the
	// barrier completes, so an operator grepping logs at 3 AM can find the
	// stalled epoch, which shards have (and have not) arrived, and how long
	// it has been stuck without having to correlate silence across shards.
	deferredMaintenanceBarrierStallLogInterval = 30 * time.Second
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

const selectDeferredMaintenanceBarrierCompletedSQL = `
SELECT completed_at
FROM deferred_maintenance_barriers
WHERE barrier_name = $1
  AND epoch = $2
`

// DeferredMaintenanceBarrierConfig identifies one sharded ingester's
// participation in the fleet-wide deferred-maintenance barrier.
type DeferredMaintenanceBarrierConfig struct {
	ShardCount int
	ShardIndex int
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
// current epoch has arrived. Single-shard runtimes run maintenance directly.
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
	epoch, err := ensureDeferredMaintenanceBarrierEpoch(ctx, tx, config.ShardCount, now)
	if err != nil {
		return err
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

// waitDeferredMaintenanceBarrierCompletion blocks until every shard has
// arrived at epoch and the leader has marked it complete, or ctx is
// cancelled. The wait is deliberately unbounded rather than falling back to a
// deadline or partial-arrival quorum: this barrier's entire purpose is to
// stop deferred relationship maintenance from running while any shard might
// still be mid-ingest, and a quorum or timeout bypass would let maintenance
// run against a fleet that has not actually reached a quiescent point,
// trading a correctness guarantee (the repo's accuracy-first priority) for
// liveness. #5852 fixed the actual cause of an indefinite wait here — a
// shard that owns no repositories and never commits used to arrive at the
// barrier once, at startup, and then never again (see
// collector.Service.Run's everCommitted latch) — so every shard that is
// still running now keeps arriving every epoch for as long as it is alive
// and connected to Postgres. What remains unbounded on purpose is the case
// this function cannot fix: a shard that has actually crashed, hung, or lost
// connectivity. Waiting shards observe that as a stall with no deadline, by
// design, rather than risk running maintenance early against live data from
// a shard that turns out not to be dead.
func (s IngestionStore) waitDeferredMaintenanceBarrierCompletion(
	ctx context.Context,
	epoch int64,
	config DeferredMaintenanceBarrierConfig,
) error {
	if s.db == nil {
		return fmt.Errorf("ingestion store db is required")
	}

	ticker := time.NewTicker(deferredMaintenanceBarrierPollInterval)
	defer ticker.Stop()
	waitStartedAt := s.now()
	nextStallLogAt := waitStartedAt.Add(deferredMaintenanceBarrierStallLogInterval)
	for {
		completed, err := deferredMaintenanceBarrierCompleted(ctx, s.db, epoch)
		if err != nil {
			return err
		}
		if completed {
			if s.Logger != nil {
				s.Logger.InfoContext(
					ctx, "deferred maintenance barrier observed completion",
					telemetry.PhaseAttr("deferred_maintenance_barrier"),
					"epoch", epoch,
					"shard_count", config.ShardCount,
					"shard_index", config.ShardIndex,
				)
			}
			return nil
		}
		if s.Logger != nil {
			if now := s.now(); !now.Before(nextStallLogAt) {
				arrivedCount, countErr := countDeferredMaintenanceBarrierArrivals(ctx, s.db, epoch)
				logArgs := []any{
					telemetry.PhaseAttr("deferred_maintenance_barrier"),
					"epoch", epoch,
					"shard_count", config.ShardCount,
					"shard_index", config.ShardIndex,
					"waited", now.Sub(waitStartedAt).String(),
				}
				if countErr != nil {
					logArgs = append(logArgs, "arrived_shards_error", countErr.Error())
				} else {
					logArgs = append(logArgs, "arrived_shards", arrivedCount)
				}
				s.Logger.WarnContext(
					ctx, "deferred maintenance barrier still waiting for completion",
					logArgs...,
				)
				nextStallLogAt = now.Add(deferredMaintenanceBarrierStallLogInterval)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func acquireDeferredMaintenanceStateBarrier(ctx context.Context, db ExecQueryer) error {
	_, err := db.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", deferredMaintenanceBarrierStateLockKey)
	return err
}

func ensureDeferredMaintenanceBarrierEpoch(
	ctx context.Context,
	tx Transaction,
	shardCount int,
	now time.Time,
) (int64, error) {
	rows, err := tx.QueryContext(ctx, selectLatestDeferredMaintenanceBarrierSQL, deferredMaintenanceBarrierName)
	if err != nil {
		return 0, fmt.Errorf("query deferred maintenance barrier: %w", err)
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
		return 0, fmt.Errorf("close deferred maintenance barrier rows: %w", err)
	}
	if scanErr != nil {
		return 0, scanErr
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("scan deferred maintenance barrier rows: %w", err)
	}

	if latestEpoch > 0 && !completedAt.Valid && latestShardCount != shardCount {
		return 0, fmt.Errorf("deferred maintenance barrier epoch %d is open with shard count %d, refusing shard count %d", latestEpoch, latestShardCount, shardCount)
	}
	if latestEpoch > 0 && !completedAt.Valid {
		return latestEpoch, nil
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
		return 0, fmt.Errorf("insert deferred maintenance barrier epoch: %w", err)
	}
	return nextEpoch, nil
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

func deferredMaintenanceBarrierCompleted(ctx context.Context, queryer Queryer, epoch int64) (bool, error) {
	rows, err := queryer.QueryContext(ctx, selectDeferredMaintenanceBarrierCompletedSQL, deferredMaintenanceBarrierName, epoch)
	if err != nil {
		return false, fmt.Errorf("query deferred maintenance barrier completion: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("scan deferred maintenance barrier completion rows: %w", err)
		}
		return false, fmt.Errorf("deferred maintenance barrier epoch %d not found", epoch)
	}
	var completedAt sql.NullTime
	if err := rows.Scan(&completedAt); err != nil {
		return false, fmt.Errorf("scan deferred maintenance barrier completion: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("scan deferred maintenance barrier completion rows: %w", err)
	}
	return completedAt.Valid, nil
}

// countDeferredMaintenanceBarrierArrivals reports how many shards have
// recorded an arrival for epoch, for the stall-watchdog log in
// waitDeferredMaintenanceBarrierCompletion. It is read-only and, unlike
// recordDeferredMaintenanceBarrierArrival, does not require a transaction —
// a waiting shard has nothing new to record, only a count to report.
func countDeferredMaintenanceBarrierArrivals(ctx context.Context, queryer Queryer, epoch int64) (int, error) {
	rows, err := queryer.QueryContext(ctx, countDeferredMaintenanceBarrierArrivalsSQL, deferredMaintenanceBarrierName, epoch)
	if err != nil {
		return 0, fmt.Errorf("count deferred maintenance barrier arrivals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("scan deferred maintenance barrier arrival rows: %w", err)
		}
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
