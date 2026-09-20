// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// defaultWriterShapeUpgradePollInterval bounds how often a losing starter
// re-checks the marker while the winner refinalizes. Ticks are two cheap
// marker SELECTs; promptness after a deploy matters more than poll savings
// because an unapplied upgrade means the fleet keeps serving stale writer
// output.
const defaultWriterShapeUpgradePollInterval = time.Minute

// WriterShapeUpgradeRunner executes the graph-writer-shape upgrade a
// reducer starter claimed, after the service is serving. It exists because
// the upgrade's refinalize waits for the reducer drain
// (rebuildreset.WaitForReducerDrain): running it synchronously inside
// reducer startup deadlocks single-replica boots into pending generations —
// the waiter is the only worker that could drain, so the wait never
// completes and startup hangs until the drain timeout (issue #6868 broke
// the Ifá fault-injection kill cells this way). The claim, fence, and
// retirement semantics are unchanged; only the phase moves from
// before-serving to beside-serving, where the drain wait is meaningful.
type WriterShapeUpgradeRunner struct {
	Handler *Handler
	Shapes  WriterShapeStore
	Key     string
	// CurrentVersion is the binary's graph-writer shape version. The runner
	// exits once the applied marker reaches it.
	CurrentVersion int
	PollInterval   time.Duration
	Wait           func(context.Context, time.Duration) error
	Logger         *slog.Logger
}

func (r *WriterShapeUpgradeRunner) validate() error {
	if r == nil {
		return fmt.Errorf("writer shape upgrade runner is required")
	}
	if r.Handler == nil {
		return fmt.Errorf("writer shape upgrade handler is required")
	}
	if r.Shapes == nil {
		return fmt.Errorf("writer shape upgrade store is required")
	}
	if r.Key == "" {
		return fmt.Errorf("writer shape upgrade key is required")
	}
	if r.CurrentVersion <= 0 {
		return fmt.Errorf("writer shape upgrade version must be positive, got %d", r.CurrentVersion)
	}
	return nil
}

func (r *WriterShapeUpgradeRunner) pollInterval() time.Duration {
	if r.PollInterval <= 0 {
		return defaultWriterShapeUpgradePollInterval
	}
	return r.PollInterval
}

func (r *WriterShapeUpgradeRunner) wait(ctx context.Context, d time.Duration) error {
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

func (r *WriterShapeUpgradeRunner) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Run executes the claimed upgrade once the marker shows it is still
// pending, then exits. A lost claim or a failed attempt only waits for the
// next tick: the winner's marker advance is what stops the loop, so a
// crashed winner is retried by the next starter rather than wedging this
// one. A cancelled context always exits nil.
func (r *WriterShapeUpgradeRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		done, err := r.RunOnce(ctx)
		if err != nil {
			r.log().WarnContext(ctx, "graph writer shape upgrade attempt failed, retrying",
				slog.String("key", r.Key),
				slog.Int("current_version", r.CurrentVersion),
				slog.String("error", err.Error()))
		} else if done {
			r.log().InfoContext(ctx, "graph writer shape upgrade applied",
				slog.String("key", r.Key),
				slog.Int("current_version", r.CurrentVersion))
			return nil
		}
		if err := r.wait(ctx, r.pollInterval()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for writer shape upgrade retry: %w", err)
		}
	}
}

// RunOnce performs one upgrade attempt and reports whether the marker is
// current afterwards. It delegates the read/claim/refinalize/mark sequence
// to Handler.EnsureGraphWriterShape, so the runner owns only lifecycle.
func (r *WriterShapeUpgradeRunner) RunOnce(ctx context.Context) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	start := time.Now()
	retired, err := r.Handler.EnsureGraphWriterShape(ctx, r.Shapes, r.Key, r.CurrentVersion)
	if err != nil {
		return false, fmt.Errorf("ensure graph writer shape version: %w", err)
	}
	if retired {
		r.log().InfoContext(ctx, "graph writer shape upgrade refinalized",
			slog.String("key", r.Key),
			slog.Int("current_version", r.CurrentVersion),
			slog.Duration("duration", time.Since(start)))
	}
	applied, err := r.Shapes.AppliedVersion(ctx, r.Key)
	if err != nil {
		return false, fmt.Errorf("read applied writer shape version: %w", err)
	}
	return applied >= r.CurrentVersion, nil
}
