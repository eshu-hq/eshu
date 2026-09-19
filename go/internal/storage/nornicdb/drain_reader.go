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

// DrainReader executes one bounded full-refresh delete step and the bounded
// existence probe that precedes a bare-label drain.
type DrainReader interface {
	// RunWrite executes one bounded drain iteration (the
	// `WITH ... LIMIT ... DETACH DELETE` statement) and returns its rows and
	// delete counters.
	RunWrite(context.Context, string, map[string]any) (DrainWriteResult, error)

	// RunProbe runs the bounded read-only existence probe that precedes a
	// bare-label retract drain (#6822): a cheap label-scoped read that
	// answers whether any node matches before the drain runs, so a retract
	// with nothing to delete never pays the whole-store-scan cost NornicDB
	// v1.3.3 charges a bare-label `DETACH DELETE` even when nothing matches.
	// RunProbe is a distinct method, not an overload of RunWrite, so a
	// command-owned wrapper (gate, timeout) can label the probe separately
	// from the drain write it precedes: implementations MUST report the
	// probe under its own backpressure label (`canonical_probe`, never
	// `canonical_retract_drain`) and its own timeout operation string
	// ("nornicdb probe timed out", never "nornicdb drain timed out").
	RunProbe(context.Context, string, map[string]any) (DrainWriteResult, error)
}
