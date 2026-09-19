// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ingesterTimeoutDrainReader gives each full-refresh DETACH DELETE drain
// iteration, and the bounded existence probe that precedes a bare-label drain
// (#6822), its own client deadline (#5198). The bounded drain loop
// (nornicdb.PhaseGroupExecutor.executeDrainLoop) deliberately bypasses the
// grouped TimeoutExecutor so one phase-wide deadline cannot cancel a correctly
// progressing multi-iteration drain; without a per-iteration deadline, though, a
// single lost Bolt response can hold one raw drain iteration (or the probe)
// open indefinitely. This wrapper applies a fresh child context to every call,
// so a stalled iteration fails after the per-statement budget while a drain
// that keeps making progress resets the budget each iteration and is never
// canceled by an earlier one. On the client deadline it returns the shared
// retryable GraphWriteTimeoutError so the reducer queue keeps its
// graph_write_timeout retry classification; the probe and the drain report
// distinct Operation strings ("nornicdb probe timed out" vs "nornicdb drain
// timed out") so an operator can tell which one stalled. A non-positive
// timeout is a passthrough (mirroring TimeoutExecutor), so an unset budget
// leaves drain behavior unchanged.
type ingesterTimeoutDrainReader struct {
	inner       retractDrainReader
	timeout     time.Duration
	timeoutHint string
}

// RunProbe bounds one bounded existence probe with a fresh child context,
// reporting a stalled probe as "nornicdb probe timed out" rather than the
// drain's "nornicdb drain timed out" label.
func (r ingesterTimeoutDrainReader) RunProbe(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (DrainWriteResult, error) {
	return r.runBounded(ctx, cypher, params, r.inner.RunProbe, "nornicdb probe timed out", "run nornicdb probe")
}

// RunWrite bounds one drain iteration with a fresh child context. It maps a
// per-iteration client deadline (parent still live) to a retryable
// GraphWriteTimeoutError, and forwards a parent-driven cancellation unchanged.
func (r ingesterTimeoutDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (DrainWriteResult, error) {
	return r.runBounded(ctx, cypher, params, r.inner.RunWrite, "nornicdb drain timed out", "run nornicdb drain")
}

// runBounded is the shared per-call deadline logic behind RunProbe and
// RunWrite: it differs only in which inner method it calls and which
// timeout-operation/error-prefix labels the resulting error carries, so the
// probe and the drain can never be confused with each other downstream.
func (r ingesterTimeoutDrainReader) runBounded(
	ctx context.Context,
	cypher string,
	params map[string]any,
	call func(context.Context, string, map[string]any) (DrainWriteResult, error),
	timeoutOperation string,
	wrapPrefix string,
) (DrainWriteResult, error) {
	if r.timeout <= 0 {
		return call(ctx, cypher, params)
	}

	boundedCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result, err := call(boundedCtx, cypher, params)
	if err == nil {
		return result, nil
	}
	if errors.Is(boundedCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return DrainWriteResult{}, sourcecypher.GraphWriteTimeoutError{
			Operation:   timeoutOperation,
			Timeout:     r.timeout,
			TimeoutHint: r.timeoutHint,
			Cause:       context.DeadlineExceeded,
		}
	}
	return DrainWriteResult{}, fmt.Errorf("%s: %w", wrapPrefix, err)
}

// gatedDrainReader wraps a retractDrainReader so each full-refresh DETACH DELETE
// drain write, and the bounded existence probe that precedes a bare-label
// drain (#6822), draws a permit from the shared canonical graph-write gate
// (#4729 / #4456). nornicDBPhaseGroupExecutor routes Drain-marked retract
// statements through drainReader.RunWrite/RunProbe on the raw executor,
// bypassing the gated inner GroupExecutor layer; without this wrapper those
// calls run ungated, so with multiple projector workers and
// ESHU_GRAPH_WRITE_MAX_IN_FLIGHT set below the worker count the drain path
// could still exceed the configured in-flight ceiling and recreate the
// NornicDB overload the gate is meant to close. Each drain iteration (and the
// probe) is a small bounded read/write, so acquiring one permit per call keeps
// the total concurrent canonical writes (fan-out ExecuteGroup + probes +
// drains, across all workers) bounded to the ceiling. The permit is released
// before the next call, so a multi-iteration per-scope drain holds at most one
// permit at a time. The probe and the drain acquire under distinct labels
// (`canonical_probe` vs `canonical_retract_drain`) so backpressure telemetry
// attributes each correctly instead of folding the probe into the drain's
// label. A nil gate makes Acquire a no-op (passthrough), so the wrapper is
// only installed when the ceiling is configured.
type gatedDrainReader struct {
	inner retractDrainReader
	gate  *sourcecypher.BackpressureGate
}

// RunProbe acquires a canonical-gate permit under the `canonical_probe` label
// for the bounded existence probe, then delegates to the wrapped reader.
// gate.Acquire is nil-safe and returns a no-op release when the gate is unset.
func (g gatedDrainReader) RunProbe(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (DrainWriteResult, error) {
	release, err := g.gate.Acquire(ctx, string(sourcecypher.OperationCanonicalProbe))
	if err != nil {
		return DrainWriteResult{}, err
	}
	defer release()
	return g.inner.RunProbe(ctx, cypher, params)
}

// RunWrite acquires a canonical-gate permit for the drain write, then delegates
// to the wrapped reader. gate.Acquire is nil-safe and returns a no-op release
// when the gate is unset.
func (g gatedDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (DrainWriteResult, error) {
	release, err := g.gate.Acquire(ctx, "canonical_retract_drain")
	if err != nil {
		return DrainWriteResult{}, err
	}
	defer release()
	return g.inner.RunWrite(ctx, cypher, params)
}
