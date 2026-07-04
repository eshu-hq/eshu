// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphbackpressure wires the cypher.BackpressureExecutor into the
// reducer, projector, and bootstrap-index graph write paths and bridges its
// signals to telemetry.
//
// It is the root-cause control for issue #3560: when the graph backend is slow,
// every reducer/projector/bootstrap-index worker can drive a concurrent write
// that exceeds its deadline, and the timeouts dead-letter recoverable work. Wrap
// and WrapExecutorWithGate bound in-flight writes so a slow backend holds its
// permits longer, which blocks additional workers at the write boundary and
// slows intake (closed-loop backpressure) instead of converting transient
// slowness into a dead-letter flood. The bound is opt-in and not a serialization
// fix: the ceiling is configurable and greater than one, so useful write
// concurrency is preserved.
//
// WrapExecutorWithGate preserves whichever grouped-write interface the wrapped
// executor implements: cypher.GroupExecutor (atomic whole-materialization
// writes, e.g. Neo4j) or cypher.PhaseGroupExecutor (bounded per-phase grouped
// writes, e.g. bootstrap-index's NornicDB canonical executor, issue #4515 Lane
// B). Preserving the narrower PhaseGroupExecutor interface through the gate is
// required: without it, CanonicalNodeWriter.Write cannot distinguish the
// wrapped executor from a plain Executor and silently falls back to
// per-statement sequential execution, a serialization regression.
//
// The package exists on its own (rather than in internal/runtime) to avoid an
// import cycle: the cypher package's internal tests import internal/runtime, so
// the observer adapter, which must import both cypher and telemetry, cannot live
// in runtime. Only the cmd layer consumes this package.
package graphbackpressure
