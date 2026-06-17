package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const defaultCodeReachabilityPollInterval = 5 * time.Second

// CodeReachabilityInputLoader loads bounded code reachability projection
// snapshots that are newer than the currently materialized read model.
type CodeReachabilityInputLoader interface {
	LoadPendingCodeReachabilityInputs(ctx context.Context, limit int) ([]CodeReachabilityProjectionInput, error)
}

// CodeReachabilityRowWriter writes materialized code reachability rows.
type CodeReachabilityRowWriter interface {
	ReplaceRepositoryRows(
		ctx context.Context,
		scopeID string,
		generationID string,
		repositoryID string,
		rows []CodeReachabilityRow,
	) error
}

// CodeReachabilityProjectionRunnerConfig configures the code reachability
// read-model runner.
type CodeReachabilityProjectionRunnerConfig struct {
	PollInterval time.Duration
	BatchLimit   int
	MaxDepth     int
}

// CodeReachabilityProjectionResult summarizes one runner cycle.
type CodeReachabilityProjectionResult struct {
	InputsProcessed int
	RowsWritten     int
	DurationSeconds float64
}

// CodeReachabilityProjectionRunner maintains code_reachability_rows from the
// active code-call projection read model.
type CodeReachabilityProjectionRunner struct {
	InputLoader CodeReachabilityInputLoader
	RowWriter   CodeReachabilityRowWriter
	Config      CodeReachabilityProjectionRunnerConfig
	Wait        func(context.Context, time.Duration) error
	Logger      *slog.Logger
}

// Run drains code reachability projection work until the context is canceled.
func (r *CodeReachabilityProjectionRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.ProcessOnce(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if result.InputsProcessed > 0 {
			continue
		}
		if err := r.wait(ctx, r.pollInterval()); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for code reachability projection work: %w", err)
		}
	}
}

// ProcessOnce processes one bounded batch of pending reachability snapshots.
func (r *CodeReachabilityProjectionRunner) ProcessOnce(
	ctx context.Context,
	now time.Time,
) (CodeReachabilityProjectionResult, error) {
	start := time.Now()
	inputs, err := r.InputLoader.LoadPendingCodeReachabilityInputs(ctx, r.batchLimit())
	if err != nil {
		return CodeReachabilityProjectionResult{}, fmt.Errorf("load code reachability inputs: %w", err)
	}
	if len(inputs) == 0 {
		return CodeReachabilityProjectionResult{DurationSeconds: time.Since(start).Seconds()}, nil
	}

	totalRows := 0
	for _, input := range inputs {
		if input.MaxDepth <= 0 {
			input.MaxDepth = r.maxDepth()
		}
		if input.ObservedAt.IsZero() {
			input.ObservedAt = now
		}
		if input.UpdatedAt.IsZero() {
			input.UpdatedAt = now
		}
		rows := BuildCodeReachabilityRows(input)
		if err := r.RowWriter.ReplaceRepositoryRows(ctx, input.ScopeID, input.GenerationID, input.RepositoryID, rows); err != nil {
			return CodeReachabilityProjectionResult{}, fmt.Errorf("write code reachability rows: %w", err)
		}
		totalRows += len(rows)
	}

	result := CodeReachabilityProjectionResult{
		InputsProcessed: len(inputs),
		RowsWritten:     totalRows,
		DurationSeconds: time.Since(start).Seconds(),
	}
	if r.Logger != nil {
		r.Logger.Info(
			"code reachability projection completed",
			slog.Int("input_count", result.InputsProcessed),
			slog.Int("row_count", result.RowsWritten),
			slog.Float64("duration_seconds", result.DurationSeconds),
		)
	}
	return result, nil
}

func (r *CodeReachabilityProjectionRunner) validate() error {
	if r == nil {
		return fmt.Errorf("code reachability projection runner is nil")
	}
	if r.InputLoader == nil {
		return fmt.Errorf("code reachability projection input loader is nil")
	}
	if r.RowWriter == nil {
		return fmt.Errorf("code reachability projection row writer is nil")
	}
	return nil
}

func (r *CodeReachabilityProjectionRunner) wait(ctx context.Context, d time.Duration) error {
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

func (r *CodeReachabilityProjectionRunner) pollInterval() time.Duration {
	if r.Config.PollInterval <= 0 {
		return defaultCodeReachabilityPollInterval
	}
	return r.Config.PollInterval
}

func (r *CodeReachabilityProjectionRunner) batchLimit() int {
	if r.Config.BatchLimit <= 0 {
		return 10
	}
	return r.Config.BatchLimit
}

func (r *CodeReachabilityProjectionRunner) maxDepth() int {
	if r.Config.MaxDepth <= 0 {
		return defaultCodeReachabilityMaxDepth
	}
	return r.Config.MaxDepth
}
