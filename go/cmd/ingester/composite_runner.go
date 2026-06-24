package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultCompositeDrainGrace bounds how long Run waits for sibling runners to
// finish their in-flight unit after one runner returns a fatal error. It is a
// safety ceiling, not a deadline siblings are expected to consume: healthy
// runners observe context cancellation and exit well within it. The window is
// long enough for a projector to finish an in-flight graph commit or a
// collector to finish a durable fact commit, and short enough that a wedged
// sibling cannot stall ingester teardown.
const defaultCompositeDrainGrace = 30 * time.Second

// errCompositeDrainTimeout marks that at least one sibling runner did not
// return within the bounded drain grace after a fatal error. It is joined onto
// the aggregated error so an operator can tell a clean drain apart from a
// forced, bounded teardown.
var errCompositeDrainTimeout = errors.New("composite runner: sibling drain exceeded grace window")

// compositeRunner runs multiple app.Runner implementations concurrently and
// supervises their shared lifecycle. The collector and projector ingester
// services run under one compositeRunner so a fatal failure in either triggers
// a bounded, graceful drain of the other instead of an unbounded fail-fast
// teardown.
//
// Error model: every runner's terminal error is aggregated with errors.Join;
// no sibling error is masked or dropped. A clean context-driven shutdown where
// all runners return nil yields a nil error. Retryable, per-unit failures are
// owned by each service's own Run loop (durable queue retry); only errors that
// escape a service's Run are treated as fatal here.
//
// Drain model: when the first fatal error arrives, the shared context is
// canceled so siblings can stop claiming new work and finish their in-flight
// unit. Run then waits up to drainGrace for siblings to return. A sibling that
// ignores cancellation cannot stall teardown past drainGrace; its slot is
// abandoned (the process exit reaps the goroutine) and errCompositeDrainTimeout
// is joined onto the result.
type compositeRunner struct {
	runners    []app.Runner
	drainGrace time.Duration
	logger     *slog.Logger // optional — nil disables structured drain logging
}

// newCompositeRunner builds a compositeRunner with the default drain grace and
// the supplied logger for drain observability. A nil logger disables the
// composite runner's own structured logging; per-service telemetry is
// unaffected.
func newCompositeRunner(logger *slog.Logger, runners ...app.Runner) compositeRunner {
	return newCompositeRunnerWithGrace(defaultCompositeDrainGrace, logger, runners...)
}

// newCompositeRunnerWithGrace builds a compositeRunner with an explicit drain
// grace. A non-positive grace falls back to defaultCompositeDrainGrace so the
// drain wait is always bounded.
func newCompositeRunnerWithGrace(drainGrace time.Duration, logger *slog.Logger, runners ...app.Runner) compositeRunner {
	if drainGrace <= 0 {
		drainGrace = defaultCompositeDrainGrace
	}
	return compositeRunner{runners: runners, drainGrace: drainGrace, logger: logger}
}

// compositeResult carries one runner's terminal outcome back to Run, tagged
// with its index so aggregation never confuses which sibling produced which
// error.
type compositeResult struct {
	index int
	err   error
}

// Run starts every runner concurrently and supervises their shared lifecycle.
// It returns nil only when all runners exit cleanly (nil error). Otherwise it
// returns the errors.Join of every terminal error, plus errCompositeDrainTimeout
// when a sibling did not drain within the bounded grace window.
func (c compositeRunner) Run(ctx context.Context) error {
	if len(c.runners) == 0 {
		return nil
	}

	grace := c.drainGrace
	if grace <= 0 {
		grace = defaultCompositeDrainGrace
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered to len(runners) so every runner goroutine can always deliver its
	// single result without blocking, even after Run abandons the receive on a
	// bounded drain timeout. This guarantees the send side never leaks.
	results := make(chan compositeResult, len(c.runners))
	for i, r := range c.runners {
		go func(index int, runner app.Runner) {
			results <- compositeResult{index: index, err: runner.Run(ctx)}
		}(i, r)
	}

	errs := make([]error, 0, len(c.runners))
	remaining := len(c.runners)
	fatalSeen := false
	var drainDeadline <-chan time.Time
	var drainTimer *time.Timer

	for remaining > 0 {
		select {
		case res := <-results:
			remaining--
			if res.err != nil {
				errs = append(errs, res.err)
				if !fatalSeen {
					fatalSeen = true
					c.logFatal(ctx, res.index, res.err, remaining)
					// Ask siblings to stop claiming new work and drain their
					// in-flight unit, then bound how long we wait for them.
					cancel()
					drainTimer = time.NewTimer(grace)
					drainDeadline = drainTimer.C
				}
			}
		case <-drainDeadline:
			// At least one sibling did not return within the grace window.
			// Abandon the wait so teardown stays bounded; process exit reaps the
			// goroutine. The buffered results channel keeps the abandoned
			// runner's eventual send non-blocking.
			c.logDrainTimeout(ctx, remaining)
			errs = append(errs, fmt.Errorf("%w: %d sibling(s) still running", errCompositeDrainTimeout, remaining))
			if drainTimer != nil {
				drainTimer.Stop()
			}
			return errors.Join(errs...)
		}
	}

	if drainTimer != nil {
		drainTimer.Stop()
	}
	return errors.Join(errs...)
}

func (c compositeRunner) logFatal(ctx context.Context, index int, err error, remaining int) {
	if c.logger == nil {
		return
	}
	c.logger.ErrorContext(
		ctx, "composite runner sibling failed; draining peers",
		slog.Int("runner_index", index),
		slog.Int("remaining_runners", remaining),
		telemetry.FailureClassAttr("composite_runner_fatal"),
		slog.Bool("retryable", false),
		slog.Duration("drain_grace", c.drainGrace),
		slog.String("error", err.Error()),
	)
}

func (c compositeRunner) logDrainTimeout(ctx context.Context, remaining int) {
	if c.logger == nil {
		return
	}
	c.logger.ErrorContext(
		ctx, "composite runner sibling drain exceeded grace window",
		slog.Int("remaining_runners", remaining),
		telemetry.FailureClassAttr("composite_runner_drain_timeout"),
		slog.Duration("drain_grace", c.drainGrace),
	)
}
