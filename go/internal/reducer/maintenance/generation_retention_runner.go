// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultGenerationRetentionPollInterval = time.Hour
)

// GenerationRetentionPolicy bounds automated cleanup of superseded source
// generations. Raw scope and generation identifiers are intentionally absent
// from the runner contract; the storage implementation owns candidate locking.
type GenerationRetentionPolicy struct {
	MinSupersededGenerations int
	MaxSupersededAge         time.Duration
	BatchGenerationLimit     int
	BatchRowLimit            int
	PolicyScope              string
	PolicyRevision           string
}

// GenerationRetentionResult summarizes one bounded cleanup transaction.
// RowsPruned is keyed by bounded table or data-class names. PhaseDurations is
// keyed by the storage implementation's closed phase names, and ScopeLockHold
// is how long the transaction held its scope row locks, the window a concurrent
// fact insert into one of those scopes waits out (#7279).
type GenerationRetentionResult struct {
	GenerationsPruned int
	RowsPruned        map[string]int64
	Skipped           map[string]int
	OldestEligibleAge time.Duration
	Duration          time.Duration
	PhaseDurations    map[string]time.Duration
	ScopeLockHold     time.Duration
}

// ErrGenerationRetentionKeyIndexUnavailable marks a cycle the pruner refused
// because an index its retention statements depend on is missing or invalid
// (#7279). The runner reports it as failure reason key_index_unavailable and
// retries on its next poll; an operator rebuilds the index to clear it.
var ErrGenerationRetentionKeyIndexUnavailable = errors.New("generation retention key index unavailable")

// GenerationRetentionPruner runs one bounded generation-retention cleanup
// transaction.
type GenerationRetentionPruner interface {
	PruneSupersededGenerations(context.Context, GenerationRetentionPolicy) (GenerationRetentionResult, error)
}

// GenerationRetentionRunnerConfig configures the retention cleanup loop.
type GenerationRetentionRunnerConfig struct {
	PollInterval time.Duration
	Policy       GenerationRetentionPolicy
}

func (c GenerationRetentionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultGenerationRetentionPollInterval
	}
	return c.PollInterval
}

// GenerationRetentionRunner prunes superseded source-generation history in
// bounded Postgres transactions beside normal reducer intent processing.
type GenerationRetentionRunner struct {
	Pruner GenerationRetentionPruner
	Config GenerationRetentionRunnerConfig
	Wait   func(context.Context, time.Duration) error

	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run drains eligible retention batches until the context is cancelled.
func (r *GenerationRetentionRunner) Run(ctx context.Context) error {
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
				if generationRetentionContextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for generation retention retry: %w", waitErr)
			}
			continue
		}
		if result.GenerationsPruned > 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if generationRetentionContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for generation retention work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded retention cleanup transaction.
func (r *GenerationRetentionRunner) RunOnce(ctx context.Context) (GenerationRetentionResult, error) {
	if err := r.validate(); err != nil {
		return GenerationRetentionResult{}, err
	}

	result, err := r.Pruner.PruneSupersededGenerations(ctx, r.Config.Policy)
	if err != nil {
		return GenerationRetentionResult{}, fmt.Errorf("prune superseded generations: %w", err)
	}
	r.recordResult(ctx, result)
	return result, nil
}

func (r *GenerationRetentionRunner) validate() error {
	if r.Pruner == nil {
		return errors.New("generation retention pruner is required")
	}
	return nil
}

func (r *GenerationRetentionRunner) wait(ctx context.Context, d time.Duration) error {
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

func (r *GenerationRetentionRunner) recordResult(ctx context.Context, result GenerationRetentionResult) {
	if r.Instruments != nil {
		if result.GenerationsPruned > 0 {
			r.Instruments.GenerationRetentionPruned.Add(ctx, int64(result.GenerationsPruned))
			r.Instruments.GenerationRetentionBatchSize.Record(ctx, int64(result.GenerationsPruned))
		}
		for tableName, rows := range result.RowsPruned {
			if rows <= 0 {
				continue
			}
			r.Instruments.GenerationRetentionRowsPruned.Add(ctx, rows, metric.WithAttributes(
				attribute.String("table", tableName),
			))
		}
		if result.Duration > 0 {
			r.Instruments.GenerationRetentionDuration.Record(ctx, result.Duration.Seconds())
		}
		if result.OldestEligibleAge > 0 {
			r.Instruments.GenerationRetentionOldestEligibleAge.Record(ctx, result.OldestEligibleAge.Seconds())
		}
		for phase, duration := range result.PhaseDurations {
			r.Instruments.GenerationRetentionPhaseDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
				attribute.String("phase", phase),
			))
		}
		if result.ScopeLockHold > 0 {
			r.Instruments.GenerationRetentionScopeLockHold.Record(ctx, result.ScopeLockHold.Seconds())
		}
		for reason, count := range result.Skipped {
			if count <= 0 {
				continue
			}
			r.Instruments.GenerationRetentionSkipped.Add(ctx, int64(count), metric.WithAttributes(
				attribute.String("reason", reason),
			))
		}
	}

	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"generation retention cycle completed",
		slog.Int("generations_pruned", result.GenerationsPruned),
		slog.Int64("rows_pruned_total", generationRetentionRowsPrunedTotal(result.RowsPruned)),
		slog.Int("skipped_total", generationRetentionSkippedTotal(result.Skipped)),
		slog.Any("skipped_by_reason", result.Skipped),
		slog.Float64("duration_seconds", result.Duration.Seconds()),
		slog.Float64("oldest_eligible_age_seconds", result.OldestEligibleAge.Seconds()),
		slog.Float64("scope_lock_hold_seconds", result.ScopeLockHold.Seconds()),
		slog.Any("phase_seconds", generationRetentionPhaseSeconds(result.PhaseDurations)),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *GenerationRetentionRunner) recordFailure(ctx context.Context, err error) {
	reason, failureClass, message := "store_error", "generation_retention_error", "generation retention cycle failed"
	if errors.Is(err, ErrGenerationRetentionKeyIndexUnavailable) {
		reason = "key_index_unavailable"
		failureClass = "generation_retention_key_index_unavailable"
		message = "generation retention cycle refused: retention key index missing or invalid"
	}
	if r.Instruments != nil {
		r.Instruments.GenerationRetentionFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", reason),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			message,
			log.Err(err),
			telemetry.FailureClassAttr(failureClass),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

// generationRetentionPhaseSeconds converts phase durations to seconds for the
// cycle log.
func generationRetentionPhaseSeconds(phases map[string]time.Duration) map[string]float64 {
	seconds := make(map[string]float64, len(phases))
	for phase, duration := range phases {
		seconds[phase] = duration.Seconds()
	}
	return seconds
}

func generationRetentionRowsPrunedTotal(rows map[string]int64) int64 {
	var total int64
	for _, count := range rows {
		total += count
	}
	return total
}

func generationRetentionSkippedTotal(skipped map[string]int) int {
	var total int
	for _, count := range skipped {
		total += count
	}
	return total
}

func generationRetentionContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
