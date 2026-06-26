// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultGraphOrphanSweepPollInterval = time.Hour
	defaultGraphOrphanSweepLeaseTTL     = 5 * time.Minute
)

const (
	graphOrphanSweepLeaseDomain         = "graph_orphan_sweep"
	graphOrphanSweepLeasePartitionID    = 0
	graphOrphanSweepLeasePartitionCount = 1
)

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
	Duration      time.Duration
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
		defer func() {
			_ = r.LeaseManager.ReleasePartitionLease(
				ctx,
				graphOrphanSweepLeaseDomain,
				graphOrphanSweepLeasePartitionID,
				graphOrphanSweepLeasePartitionCount,
				r.Config.leaseOwner(),
			)
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
		slog.Int64("orphan_count_total", graphOrphanSweepTotal(result.Counts)),
		slog.Int64("marked_total", graphOrphanSweepTotal(result.Marked)),
		slog.Int64("deleted_total", graphOrphanSweepTotal(result.Deleted)),
		slog.Any("counts_by_label", result.Counts),
		slog.Any("marked_by_label", result.Marked),
		slog.Any("deleted_by_label", result.Deleted),
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
