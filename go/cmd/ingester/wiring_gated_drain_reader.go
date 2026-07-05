// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

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
