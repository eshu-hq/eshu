// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import "context"

// DrainWriteResult carries rows and delete counters from one bounded drain step.
type DrainWriteResult struct {
	Rows                 []map[string]any
	NodesDeleted         int64
	RelationshipsDeleted int64
}

// DrainReader executes one bounded full-refresh delete step for a
// Drain-marked canonical retract statement.
//
// The bounded read-only existence probe that precedes a bare-label drain
// (#6822) is NOT a DrainReader method: it dispatches through
// PhaseGroupExecutor.Inner as a sourcecypher.ProbeExecutor carrying
// sourcecypher.OperationCanonicalProbe (#6852), the same seam every other
// probe-guarded retract in this codebase uses. That seam runs the probe
// through the instrumented executor chain (backpressure, timeout,
// InstrumentedExecutor, RetryingExecutor), so it gets the same
// `neo4j.execute_probe` span and `Neo4jQueryDuration{operation=probe}` point
// as any other probe, instead of the bespoke gate label and timeout error the
// former DrainReader.RunProbe method carried on its own.
type DrainReader interface {
	// RunWrite executes one bounded drain iteration (the
	// `WITH ... LIMIT ... DETACH DELETE` statement) and returns its rows and
	// delete counters.
	RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error)
}
