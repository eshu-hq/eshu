// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison

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

const defaultPoisonLivenessPollInterval = 5 * time.Minute

// Policy bounds the poison dead-letter recovery arm (#4740). It
// governs re-enqueuing a fresh pending attempt for a fact_work_items row that
// is already terminally 'dead_letter' with no newer scope generation — the
// class liveness.Runner does not reach, because such a scope has no
// ACTIVE generation to begin with.
type Policy struct {
	// MaxRecoverAttempts bounds the automated re-drive budget per work item so a
	// genuinely poison item cannot loop forever.
	MaxRecoverAttempts int
	// BatchLimit caps how many dead-letter rows one sweep re-drives.
	BatchLimit int
}

// Result summarizes one bounded poison-recovery sweep.
type Result struct {
	// Recovered counts dead-letter rows re-enqueued to pending this cycle.
	Recovered int
}

// Recoverer runs one bounded poison-recovery sweep against the
// dead-letter/poison class.
type Recoverer interface {
	RecoverPoisonDeadLetters(context.Context, Policy, time.Time) (Result, error)
}

// Config configures the poison-recovery sweep loop.
//
// AutoRetryEnabled gates whether the sweep loop actually re-drives dead-letter
// rows (bounded auto-retry) or is disabled entirely. The default operational
// posture (#4740) is surface-only: the stuck-gauge (registered independently
// via telemetry.RegisterPoisonLivenessObservableGauges, which does not depend
// on this runner) always reports the poison class regardless of this flag, but
// the runner itself — and therefore any re-enqueue write — only runs when an
// operator opts in.
type Config struct {
	// AutoRetryEnabled opts into the bounded auto-retry sweep. When false, the
	// runner is not constructed at all (see cmd/reducer wiring); the poison
	// class is still visible via the gauge, just not automatically re-driven.
	AutoRetryEnabled bool
	PollInterval     time.Duration
	Policy           Policy
}

func (c Config) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultPoisonLivenessPollInterval
	}
	return c.PollInterval
}

// Runner runs the bounded auto-retry sweep for the
// dead-letter/poison class beside normal reducer intent processing. It only
// re-drives a row when Config.AutoRetryEnabled is true; the
// stuck-gauge that reports the class size is independent of this runner and
// always active.
type Runner struct {
	Recoverer Recoverer
	Config    Config
	Now       func() time.Time
	Wait      func(context.Context, time.Duration) error

	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run sweeps the poison dead-letter class until the context is cancelled. A
// cycle that recovers work loops immediately so a backlog drains; an empty
// cycle waits a poll interval before the next sweep.
func (r *Runner) Run(ctx context.Context) error {
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
				if poisonLivenessContextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for poison liveness retry: %w", waitErr)
			}
			continue
		}
		if result.Recovered > 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if poisonLivenessContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for poison liveness work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded poison-recovery sweep.
func (r *Runner) RunOnce(ctx context.Context) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}

	result, err := r.Recoverer.RecoverPoisonDeadLetters(ctx, r.Config.Policy, r.now())
	if err != nil {
		return Result{}, fmt.Errorf("recover poison dead letters: %w", err)
	}
	r.recordResult(ctx, result)
	return result, nil
}

func (r *Runner) validate() error {
	if r.Recoverer == nil {
		return errors.New("poison liveness recoverer is required")
	}
	return nil
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) wait(ctx context.Context, d time.Duration) error {
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

func (r *Runner) recordResult(ctx context.Context, result Result) {
	if r.Instruments != nil && result.Recovered > 0 {
		r.Instruments.PoisonLivenessRecovered.Add(ctx, int64(result.Recovered))
	}
	if r.Logger == nil || result.Recovered == 0 {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"poison dead-letter recovery cycle completed",
		slog.Int("items_recovered", result.Recovered),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *Runner) recordFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.PoisonLivenessFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "store_error"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"poison dead-letter recovery cycle failed",
			log.Err(err),
			telemetry.FailureClassAttr("poison_liveness_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func poisonLivenessContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
