// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	deferredMaintenanceBarrierPollInterval = 250 * time.Millisecond
	// deferredMaintenanceBarrierStallLogInterval bounds how often a shard
	// stuck in waitDeferredMaintenanceBarrierCompletion re-announces the
	// stall. The wait itself stays unbounded (see the function doc for why),
	// but a shard that has been waiting this long or longer logs a WARN with
	// the current arrival state on every subsequent poll tick until the
	// barrier completes, so an operator grepping logs at 3 AM can find the
	// stalled epoch, which shards have (and have not) arrived, and how long
	// it has been stuck without having to correlate silence across shards.
	deferredMaintenanceBarrierStallLogInterval = 30 * time.Second
)

const selectDeferredMaintenanceBarrierCompletedSQL = `
SELECT completed_at
FROM deferred_maintenance_barriers
WHERE barrier_name = $1
  AND epoch = $2
`

const selectDeferredMaintenanceBarrierArrivedShardIndexesSQL = `
SELECT shard_index
FROM deferred_maintenance_barrier_arrivals
WHERE barrier_name = $1
  AND epoch = $2
ORDER BY shard_index
`

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
// and connected to Postgres, as soon as one is open for it to join (see
// DeferredMaintenanceBarrierConfig.HasCommitted for the join-only rule that
// keeps a never-committing shard from opening epochs on its own — without
// it, the same fix would run maintenance against an unchanged corpus on
// every idle poll of a quiet fleet). What remains unbounded on purpose is
// the case this function cannot fix: a shard that has actually crashed,
// hung, or lost connectivity. Waiting shards observe that as a stall with no
// deadline, by design, rather than risk running maintenance early against
// live data from a shard that turns out not to be dead.
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
				arrivedIndexes, listErr := deferredMaintenanceBarrierArrivedShardIndexes(ctx, s.db, epoch)
				logArgs := []any{
					telemetry.PhaseAttr("deferred_maintenance_barrier"),
					"epoch", epoch,
					"shard_count", config.ShardCount,
					"shard_index", config.ShardIndex,
					"waited", now.Sub(waitStartedAt).String(),
				}
				if listErr != nil {
					logArgs = append(logArgs, "arrived_shards_error", listErr.Error())
				} else {
					// Name both sets explicitly: an operator reading
					// shard_count=5, arrived_shards=3 alone still cannot tell
					// WHICH two shards are silent without correlating logs
					// across every process. missing_shard_indexes answers that
					// directly from this one log line.
					logArgs = append(logArgs,
						"arrived_shards", len(arrivedIndexes),
						"arrived_shard_indexes", arrivedIndexes,
						"missing_shard_indexes", missingDeferredMaintenanceBarrierShardIndexes(config.ShardCount, arrivedIndexes),
					)
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

// deferredMaintenanceBarrierArrivedShardIndexes reports which shard indexes
// have recorded an arrival for epoch, sorted ascending, for the
// stall-watchdog log in waitDeferredMaintenanceBarrierCompletion. It is
// read-only and, unlike recordDeferredMaintenanceBarrierArrival, does not
// require a transaction — a waiting shard has nothing new to record, only
// arrivals to report. Reporting indexes rather than only a count lets the
// stall warning name the specific missing shards (see
// missingDeferredMaintenanceBarrierShardIndexes) instead of leaving an
// operator to correlate log silence across every shard process by hand.
func deferredMaintenanceBarrierArrivedShardIndexes(ctx context.Context, queryer Queryer, epoch int64) ([]int, error) {
	rows, err := queryer.QueryContext(ctx, selectDeferredMaintenanceBarrierArrivedShardIndexesSQL, deferredMaintenanceBarrierName, epoch)
	if err != nil {
		return nil, fmt.Errorf("list deferred maintenance barrier arrived shards: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var indexes []int
	for rows.Next() {
		var shardIndex int
		if err := rows.Scan(&shardIndex); err != nil {
			return nil, fmt.Errorf("scan deferred maintenance barrier arrived shard: %w", err)
		}
		indexes = append(indexes, shardIndex)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan deferred maintenance barrier arrived shard rows: %w", err)
	}
	return indexes, nil
}

// missingDeferredMaintenanceBarrierShardIndexes returns, sorted ascending,
// every shard index in [0, shardCount) that is absent from arrived. arrived
// is expected sorted ascending (deferredMaintenanceBarrierArrivedShardIndexes
// guarantees this via ORDER BY), which keeps this a single linear pass.
func missingDeferredMaintenanceBarrierShardIndexes(shardCount int, arrived []int) []int {
	present := make(map[int]bool, len(arrived))
	for _, idx := range arrived {
		present[idx] = true
	}
	missing := make([]int, 0, shardCount-len(arrived))
	for shardIndex := 0; shardIndex < shardCount; shardIndex++ {
		if !present[shardIndex] {
			missing = append(missing, shardIndex)
		}
	}
	return missing
}
