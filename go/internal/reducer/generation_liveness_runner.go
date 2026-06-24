package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const defaultGenerationLivenessPollInterval = 5 * time.Minute

// GenerationLivenessPolicy bounds the liveness sweep that recovers wedged
// active generations. Raw scope and generation identifiers are intentionally
// absent from the runner contract; the storage implementation owns candidate
// selection and locking.
type GenerationLivenessPolicy struct {
	// ActivationDeadline is how long an active generation may make no forward
	// progress past canonical-nodes-committed before it is treated as wedged.
	ActivationDeadline time.Duration
	// MaxRecoverAttempts bounds the automated re-drive budget per generation.
	MaxRecoverAttempts int
	// BatchLimit caps how many generations one sweep retires or re-drives.
	BatchLimit int
}

// GenerationLivenessResult summarizes one liveness sweep.
type GenerationLivenessResult struct {
	// Superseded counts orphaned older active generations retired this cycle.
	Superseded int
	// Recovered counts wedged active generations re-driven through projector
	// re-enqueue this cycle.
	Recovered int
}

// GenerationLivenessRecoverer runs one bounded liveness recovery sweep over the
// active generation set.
type GenerationLivenessRecoverer interface {
	RecoverWedgedGenerations(context.Context, GenerationLivenessPolicy, time.Time) (GenerationLivenessResult, error)
}

// GenerationLivenessRunnerConfig configures the liveness sweep loop.
type GenerationLivenessRunnerConfig struct {
	PollInterval time.Duration
	Policy       GenerationLivenessPolicy
}

func (c GenerationLivenessRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultGenerationLivenessPollInterval
	}
	return c.PollInterval
}

// GenerationLivenessRunner detects and recovers wedged active generations
// beside normal reducer intent processing. It is the self-healing path for the
// generation lifecycle: an active generation that makes no forward progress
// past canonical-nodes-committed within the activation deadline is flagged
// (metric + log) and re-driven through projector re-enqueue rather than left
// active indefinitely.
type GenerationLivenessRunner struct {
	Recoverer GenerationLivenessRecoverer
	Config    GenerationLivenessRunnerConfig
	Now       func() time.Time
	Wait      func(context.Context, time.Duration) error

	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run sweeps eligible wedged generations until the context is cancelled. A
// cycle that recovers or supersedes work loops immediately so a backlog drains;
// an empty cycle waits a poll interval before the next sweep.
func (r *GenerationLivenessRunner) Run(ctx context.Context) error {
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
				if generationLivenessContextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for generation liveness retry: %w", waitErr)
			}
			continue
		}
		if result.Recovered > 0 || result.Superseded > 0 {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if generationLivenessContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for generation liveness work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded liveness recovery sweep.
func (r *GenerationLivenessRunner) RunOnce(ctx context.Context) (GenerationLivenessResult, error) {
	if err := r.validate(); err != nil {
		return GenerationLivenessResult{}, err
	}

	result, err := r.Recoverer.RecoverWedgedGenerations(ctx, r.Config.Policy, r.now())
	if err != nil {
		return GenerationLivenessResult{}, fmt.Errorf("recover wedged generations: %w", err)
	}
	r.recordResult(ctx, result)
	return result, nil
}

func (r *GenerationLivenessRunner) validate() error {
	if r.Recoverer == nil {
		return errors.New("generation liveness recoverer is required")
	}
	return nil
}

func (r *GenerationLivenessRunner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *GenerationLivenessRunner) wait(ctx context.Context, d time.Duration) error {
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

func (r *GenerationLivenessRunner) recordResult(ctx context.Context, result GenerationLivenessResult) {
	if r.Instruments != nil {
		if result.Recovered > 0 {
			r.Instruments.GenerationLivenessRecovered.Add(ctx, int64(result.Recovered))
		}
		if result.Superseded > 0 {
			r.Instruments.GenerationLivenessSuperseded.Add(ctx, int64(result.Superseded))
		}
	}
	if r.Logger == nil {
		return
	}
	if result.Recovered == 0 && result.Superseded == 0 {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"generation liveness recovery cycle completed",
		slog.Int("generations_recovered", result.Recovered),
		slog.Int("generations_superseded", result.Superseded),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *GenerationLivenessRunner) recordFailure(ctx context.Context, err error) {
	if r.Instruments != nil {
		r.Instruments.GenerationLivenessFailures.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "store_error"),
		))
	}
	if r.Logger != nil {
		r.Logger.ErrorContext(
			ctx,
			"generation liveness recovery cycle failed",
			slog.String("error", err.Error()),
			telemetry.FailureClassAttr("generation_liveness_error"),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

func generationLivenessContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
