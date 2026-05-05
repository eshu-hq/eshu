package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	defaultGraphProjectionRepairPollInterval = time.Second
	defaultGraphProjectionRepairBatchLimit   = 100
	defaultGraphProjectionRepairRetryDelay   = time.Minute
	maxGraphProjectionRepairPollInterval     = 5 * time.Second
)

// GraphProjectionPhaseStateLookup resolves whether one exact readiness phase is
// already published.
type GraphProjectionPhaseStateLookup interface {
	Lookup(context.Context, GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool, error)
}

// GraphProjectionPhaseRepairerConfig controls the reducer-side readiness
// repair loop.
type GraphProjectionPhaseRepairerConfig struct {
	PollInterval time.Duration
	BatchLimit   int
	RetryDelay   time.Duration
}

func (c GraphProjectionPhaseRepairerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultGraphProjectionRepairPollInterval
	}
	return c.PollInterval
}

func (c GraphProjectionPhaseRepairerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return defaultGraphProjectionRepairBatchLimit
	}
	return c.BatchLimit
}

func (c GraphProjectionPhaseRepairerConfig) retryDelay() time.Duration {
	if c.RetryDelay <= 0 {
		return defaultGraphProjectionRepairRetryDelay
	}
	return c.RetryDelay
}

// GraphProjectionPhaseRepairer drains the exact repair queue and republishes
// missing readiness rows when the bounded generation is still authoritative.
type GraphProjectionPhaseRepairer struct {
	Queue       GraphProjectionPhaseRepairQueue
	AcceptedGen AcceptedGenerationLookup
	StateLookup GraphProjectionPhaseStateLookup
	Publisher   GraphProjectionPhasePublisher
	Config      GraphProjectionPhaseRepairerConfig
	Wait        func(context.Context, time.Duration) error

	Tracer      any
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Run polls the repair queue until the context is cancelled.
func (r *GraphProjectionPhaseRepairer) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	consecutiveEmpty := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		startedAt := time.Now()
		repaired, err := r.RunOnce(ctx, startedAt.UTC())
		if err != nil {
			if r.Logger != nil {
				r.Logger.ErrorContext(
					ctx,
					"graph projection readiness repair cycle failed",
					slog.String("error", err.Error()),
					slog.Float64("duration_seconds", time.Since(startedAt).Seconds()),
					telemetry.FailureClassAttr("graph_projection_repair_cycle_error"),
					telemetry.PhaseAttr(telemetry.PhaseShared),
				)
			}
			consecutiveEmpty++
			if err := r.wait(ctx, graphProjectionRepairBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("wait for graph projection repair work: %w", err)
			}
			continue
		}

		if repaired > 0 {
			consecutiveEmpty = 0
			continue
		}

		consecutiveEmpty++
		if err := r.wait(ctx, graphProjectionRepairBackoff(r.Config.pollInterval(), consecutiveEmpty)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for graph projection repair work: %w", err)
		}
	}
}

// RunOnce processes one bounded repair batch and returns the number of repaired
// readiness rows published during the cycle.
func (r *GraphProjectionPhaseRepairer) RunOnce(ctx context.Context, now time.Time) (int, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}

	repairs, err := r.Queue.ListDue(ctx, now, r.Config.batchLimit())
	if err != nil {
		return 0, fmt.Errorf("list due graph projection repairs: %w", err)
	}
	if len(repairs) == 0 {
		return 0, nil
	}

	repairedCount := 0
	for _, repair := range repairs {
		if err := repair.Validate(); err != nil {
			return repairedCount, fmt.Errorf("validate graph projection repair: %w", err)
		}

		ready, found, err := r.StateLookup.Lookup(ctx, repair.Key, repair.Phase)
		if err != nil {
			return repairedCount, fmt.Errorf("lookup graph projection phase state: %w", err)
		}
		if found && ready {
			if err := r.Queue.Delete(ctx, []GraphProjectionPhaseRepair{repair}); err != nil {
				return repairedCount, fmt.Errorf("delete already-repaired graph projection row: %w", err)
			}
			continue
		}

		acceptanceKey := SharedProjectionAcceptanceKey{
			ScopeID:          repair.Key.ScopeID,
			AcceptanceUnitID: repair.Key.AcceptanceUnitID,
			SourceRunID:      repair.Key.SourceRunID,
		}
		acceptedGeneration, ok := r.AcceptedGen(acceptanceKey)
		if !ok || acceptedGeneration != repair.Key.GenerationID {
			if err := r.Queue.Delete(ctx, []GraphProjectionPhaseRepair{repair}); err != nil {
				return repairedCount, fmt.Errorf("delete stale graph projection repair row: %w", err)
			}
			continue
		}

		state := GraphProjectionPhaseState{
			Key:         repair.Key,
			Phase:       repair.Phase,
			CommittedAt: repair.CommittedAt,
			UpdatedAt:   now.UTC(),
		}
		if err := r.Publisher.PublishGraphProjectionPhases(ctx, []GraphProjectionPhaseState{state}); err != nil {
			nextAttemptAt := now.Add(r.Config.retryDelay()).UTC()
			if markErr := r.Queue.MarkFailed(ctx, repair, nextAttemptAt, err.Error()); markErr != nil {
				return repairedCount, fmt.Errorf("mark graph projection repair failed: %w", markErr)
			}
			if r.Logger != nil {
				logAttrs := repairLogAttrs(repair)
				logAttrs = append(logAttrs,
					slog.String("error", err.Error()),
					slog.Time("next_attempt_at", nextAttemptAt),
					telemetry.FailureClassAttr("graph_projection_repair_publish_failed"),
					telemetry.PhaseAttr(telemetry.PhaseShared),
				)
				r.Logger.ErrorContext(ctx, "graph projection readiness repair publish failed", logAttrs...)
			}
			continue
		}

		if err := r.Queue.Delete(ctx, []GraphProjectionPhaseRepair{repair}); err != nil {
			return repairedCount, fmt.Errorf("delete repaired graph projection row: %w", err)
		}
		repairedCount++
		if r.Logger != nil {
			logAttrs := repairLogAttrs(repair)
			logAttrs = append(logAttrs,
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
			r.Logger.InfoContext(ctx, "graph projection readiness repaired", logAttrs...)
		}
	}

	if repairedCount > 0 {
		if r.Instruments != nil {
			r.Instruments.CanonicalWrites.Add(
				ctx,
				int64(repairedCount),
				metric.WithAttributes(telemetry.AttrDomain("graph_projection_repair")),
			)
		}
		if r.Logger != nil {
			r.Logger.InfoContext(
				ctx,
				"graph projection readiness repair cycle completed",
				slog.Int("repaired_count", repairedCount),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}
	}

	return repairedCount, nil
}

func (r *GraphProjectionPhaseRepairer) validate() error {
	if r.Queue == nil {
		return errors.New("graph projection repairer: queue is required")
	}
	if r.AcceptedGen == nil {
		return errors.New("graph projection repairer: accepted generation lookup is required")
	}
	if r.StateLookup == nil {
		return errors.New("graph projection repairer: state lookup is required")
	}
	if r.Publisher == nil {
		return errors.New("graph projection repairer: publisher is required")
	}
	return nil
}

func (r *GraphProjectionPhaseRepairer) wait(ctx context.Context, interval time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func graphProjectionRepairBackoff(base time.Duration, consecutiveEmpty int) time.Duration {
	backoff := base
	for i := 1; i < consecutiveEmpty && i < 4; i++ {
		backoff *= 2
	}
	if backoff > maxGraphProjectionRepairPollInterval {
		return maxGraphProjectionRepairPollInterval
	}
	return backoff
}

func repairLogAttrs(repair GraphProjectionPhaseRepair) []any {
	logAttrs := make([]any, 0, 8)
	for _, attr := range telemetry.AcceptanceAttrs(
		repair.Key.ScopeID,
		repair.Key.AcceptanceUnitID,
		repair.Key.SourceRunID,
		repair.Key.GenerationID,
	) {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(logAttrs,
		slog.String("keyspace", string(repair.Key.Keyspace)),
		slog.String("graph_projection_phase", string(repair.Phase)),
	)
	return logAttrs
}
