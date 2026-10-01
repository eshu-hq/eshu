// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// defaultGraphOrphanSweepLeaseTTL is 10 minutes so one sweep cycle's lease
// outlasts the graph write budget with margin: ops-qa's
// ESHU_CANONICAL_WRITE_TIMEOUT is 300s, and a 5m lease reached the end of the
// lease with no margin when a write ran to its full budget, admitting a
// second holder mid-cycle (#7047). 10m keeps crash failover delay small
// against the hourly poll. If a cycle ever outgrows a fixed TTL, the renewal
// heartbeat precedent is ProcessPartitionOnce's TTL/2 same-owner re-claim
// (#4449), not a longer TTL.
const (
	defaultGraphOrphanSweepPollInterval = time.Hour
	defaultGraphOrphanSweepLeaseTTL     = 10 * time.Minute
)

// PartitionLeaseManager manages partition leases for the graph orphan sweep.
// It mirrors reducer.PartitionLeaseManager (shared_projection_worker.go)
// method-for-method. It is declared locally rather than imported from the
// reducer root: the root's own PartitionLeaseManager is genuine root-owned
// logic shared by several families that have not moved out of root yet, so
// importing it would violate the rule that a family subpackage never imports
// the reducer root (issue #6061). Go interfaces are satisfied structurally,
// so the same concrete lease-store implementation root wires into other
// families' readers also satisfies this local declaration without any code
// duplication.
type PartitionLeaseManager interface {
	ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string, leaseTTL time.Duration) (bool, error)
	ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, leaseOwner string) error
}

const (
	graphOrphanSweepLeaseDomain         = "graph_orphan_sweep"
	graphOrphanSweepLeasePartitionID    = 0
	graphOrphanSweepLeasePartitionCount = 1
)

// graphOrphanSweepLeaseReleaseTimeout bounds the lease release that runs
// after the cycle's own context has been canceled: long enough for one
// Postgres round trip on a loaded host, short enough that shutdown cannot
// hang on a dead backend. It mirrors repoDependencyLeaseReleaseTimeout.
const graphOrphanSweepLeaseReleaseTimeout = 10 * time.Second

// ErrGraphOrphanSweeperRequired reports missing graph orphan sweep wiring.
var ErrGraphOrphanSweeperRequired = errors.New("graph orphan sweeper is required")

// GraphOrphanSweepPolicy bounds zero-relationship graph node cleanup.
type GraphOrphanSweepPolicy struct {
	OrphanTTL  time.Duration
	BatchLimit int
	CountLimit int
	Labels     []string
}

// GraphOrphanSweepResult summarizes one bounded graph orphan cleanup cycle.
type GraphOrphanSweepResult struct {
	LeaseAcquired bool
	Counts        map[string]int64
	Marked        map[string]int64
	Deleted       map[string]int64
	// Skipped counts write statements that were not executed because
	// a preceding cheap count query returned zero.
	Skipped  map[string]int64
	Duration time.Duration
}

// GraphOrphanSweeper runs one bounded orphan-node cleanup cycle.
type GraphOrphanSweeper interface {
	SweepOrphanNodes(context.Context, GraphOrphanSweepPolicy) (GraphOrphanSweepResult, error)
}

// GraphOrphanSweepRunnerConfig configures the graph orphan cleanup loop.
type GraphOrphanSweepRunnerConfig struct {
	PollInterval time.Duration
	LeaseOwner   string
	LeaseTTL     time.Duration
	Policy       GraphOrphanSweepPolicy
}

func (c GraphOrphanSweepRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultGraphOrphanSweepPollInterval
	}
	return c.PollInterval
}

func (c GraphOrphanSweepRunnerConfig) leaseOwner() string {
	return c.LeaseOwner
}

func (c GraphOrphanSweepRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultGraphOrphanSweepLeaseTTL
	}
	return c.LeaseTTL
}

// GraphOrphanSweepRunner sweeps aged zero-relationship graph nodes beside the
// reducer's normal intent processing.
type GraphOrphanSweepRunner struct {
	Sweeper      GraphOrphanSweeper
	LeaseManager PartitionLeaseManager
	Config       GraphOrphanSweepRunnerConfig
	Wait         func(context.Context, time.Duration) error

	Logger *slog.Logger
}

// Run drains eligible orphan sweep batches until the context is cancelled.
func (r *GraphOrphanSweepRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.RunOnce(ctx)
		if err != nil {
			r.recordFailure(ctx, err)
			if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
				if graphOrphanSweepContextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for graph orphan sweep retry: %w", waitErr)
			}
			continue
		}
		if graphOrphanSweepTotal(result.Deleted) > 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if graphOrphanSweepContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for graph orphan sweep work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded graph orphan cleanup cycle.
func (r *GraphOrphanSweepRunner) RunOnce(ctx context.Context) (GraphOrphanSweepResult, error) {
	if err := r.validate(); err != nil {
		return GraphOrphanSweepResult{}, err
	}
	if r.LeaseManager != nil {
		claimed, err := r.LeaseManager.ClaimPartitionLease(
			ctx,
			graphOrphanSweepLeaseDomain,
			graphOrphanSweepLeasePartitionID,
			graphOrphanSweepLeasePartitionCount,
			r.Config.leaseOwner(),
			r.Config.leaseTTL(),
		)
		if err != nil {
			return GraphOrphanSweepResult{}, fmt.Errorf("claim graph orphan sweep lease: %w", err)
		}
		if !claimed {
			return GraphOrphanSweepResult{LeaseAcquired: false}, nil
		}
		// Release through a context that survives the cycle's own
		// cancellation: Service.Run cancels the shared context before
		// waiting for side runners, so releasing through ctx hands
		// Postgres an already-dead request and strands the lease for its
		// full TTL (#6747 shape A).
		defer func() {
			releaseCtx, releaseCancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				graphOrphanSweepLeaseReleaseTimeout,
			)
			defer releaseCancel()
			if err := r.LeaseManager.ReleasePartitionLease(
				releaseCtx,
				graphOrphanSweepLeaseDomain,
				graphOrphanSweepLeasePartitionID,
				graphOrphanSweepLeasePartitionCount,
				r.Config.leaseOwner(),
			); err != nil && r.Logger != nil {
				r.Logger.WarnContext(
					releaseCtx,
					"graph orphan sweep partition lease release failed; the lease expires on its TTL",
					slog.Int("partition_id", graphOrphanSweepLeasePartitionID),
					slog.Int("partition_count", graphOrphanSweepLeasePartitionCount),
					slog.String("lease_owner", r.Config.leaseOwner()),
					slog.Float64(telemetry.LogKeyLeaseTTLSeconds, r.Config.leaseTTL().Seconds()),
					log.Err(err),
					telemetry.PhaseAttr(telemetry.PhaseReduction),
				)
			}
		}()
	}
	result, err := r.Sweeper.SweepOrphanNodes(ctx, r.Config.Policy)
	if err != nil {
		return GraphOrphanSweepResult{}, fmt.Errorf("sweep graph orphan nodes: %w", err)
	}
	result.LeaseAcquired = true
	r.recordResult(ctx, result)
	return result, nil
}

func (r *GraphOrphanSweepRunner) validate() error {
	if r.Sweeper == nil {
		return ErrGraphOrphanSweeperRequired
	}
	if r.LeaseManager != nil && r.Config.leaseOwner() == "" {
		return fmt.Errorf("graph orphan sweep lease owner is required")
	}
	return nil
}

func (r *GraphOrphanSweepRunner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
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

func (r *GraphOrphanSweepRunner) recordResult(ctx context.Context, result GraphOrphanSweepResult) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"graph orphan sweep cycle completed",
		slog.Bool("lease_acquired", result.LeaseAcquired),
		slog.Float64(telemetry.LogKeyLeaseTTLSeconds, r.Config.leaseTTL().Seconds()),
		slog.Int64("orphan_count_total", graphOrphanSweepTotal(result.Counts)),
		slog.Int64("marked_total", graphOrphanSweepTotal(result.Marked)),
		slog.Int64("deleted_total", graphOrphanSweepTotal(result.Deleted)),
		slog.Any("counts_by_label", result.Counts),
		slog.Any("marked_by_label", result.Marked),
		slog.Any("deleted_by_label", result.Deleted),
		slog.Int64("writes_skipped_total", graphOrphanSweepTotal(result.Skipped)),
		slog.Any("skipped_by_label", result.Skipped),
		slog.Float64("duration_seconds", result.Duration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *GraphOrphanSweepRunner) recordFailure(ctx context.Context, err error) {
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"graph orphan sweep cycle failed",
			log.Err(err),
			slog.Float64(telemetry.LogKeyLeaseTTLSeconds, r.Config.leaseTTL().Seconds()),
			telemetry.FailureClassAttr("graph_orphan_sweep_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func graphOrphanSweepTotal(values map[string]int64) int64 {
	var total int64
	for _, count := range values {
		total += count
	}
	return total
}

func graphOrphanSweepContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
