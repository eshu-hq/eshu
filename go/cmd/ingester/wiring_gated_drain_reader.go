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
// iteration its own client deadline (#5198). The bounded drain loop
// (nornicdb.PhaseGroupExecutor.executeDrainLoop) deliberately bypasses the
// grouped TimeoutExecutor so one phase-wide deadline cannot cancel a correctly
// progressing multi-iteration drain; without a per-iteration deadline, though, a
// single lost Bolt response can hold one raw drain iteration open indefinitely.
// This wrapper applies a fresh child context to every RunWrite, so a stalled
// iteration fails after the per-statement budget while a drain that keeps making
// progress resets the budget each iteration and is never canceled by an earlier
// one. On the client deadline it returns the shared retryable
// GraphWriteTimeoutError so the reducer queue keeps its graph_write_timeout retry
// classification. A non-positive timeout is a passthrough (mirroring
// TimeoutExecutor), so an unset budget leaves drain behavior unchanged.
type ingesterTimeoutDrainReader struct {
	inner       retractDrainReader
	timeout     time.Duration
	timeoutHint string
}

// RunWrite bounds one drain iteration with a fresh child context. It maps a
// per-iteration client deadline (parent still live) to a retryable
// GraphWriteTimeoutError, and forwards a parent-driven cancellation unchanged.
func (r ingesterTimeoutDrainReader) RunWrite(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (DrainWriteResult, error) {
	if r.timeout <= 0 {
		return r.inner.RunWrite(ctx, cypher, params)
	}

	boundedCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result, err := r.inner.RunWrite(boundedCtx, cypher, params)
	if err == nil {
		return result, nil
	}
	if errors.Is(boundedCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return DrainWriteResult{}, sourcecypher.GraphWriteTimeoutError{
			Operation:   "nornicdb drain timed out",
			Timeout:     r.timeout,
			TimeoutHint: r.timeoutHint,
			Cause:       context.DeadlineExceeded,
		}
	}
	return DrainWriteResult{}, fmt.Errorf("run nornicdb drain: %w", err)
}

// gatedDrainReader wraps a retractDrainReader so each full-refresh DETACH DELETE
// drain write draws a permit from the shared canonical graph-write gate
// (#4729 / #4456). nornicDBPhaseGroupExecutor routes Drain-marked retract
// statements through drainReader.RunWrite on the raw executor, bypassing the
// gated inner GroupExecutor layer; without this wrapper those DELETE drains run
// ungated, so with multiple projector workers and ESHU_GRAPH_WRITE_MAX_IN_FLIGHT
// set below the worker count the drain path could still exceed the configured
// in-flight ceiling and recreate the NornicDB overload the gate is meant to
// close. Each drain iteration is a small bounded batch, so acquiring one permit
// per RunWrite keeps the total concurrent canonical writes (fan-out ExecuteGroup
// + drains, across all workers) bounded to the ceiling. The permit is released
// before the next iteration, so a multi-iteration per-scope drain holds at most
// one permit at a time. A nil gate makes Acquire a no-op (passthrough), so the
// wrapper is only installed when the ceiling is configured.
type gatedDrainReader struct {
	inner retractDrainReader
	gate  *sourcecypher.BackpressureGate
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
