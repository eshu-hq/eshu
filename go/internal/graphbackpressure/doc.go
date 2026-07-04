// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graphbackpressure wires the cypher.BackpressureExecutor into the
// reducer, projector, and bootstrap-index graph write paths and bridges its
// signals to telemetry.
//
// It is the root-cause control for issue #3560: when the graph backend is slow,
// every reducer/projector/bootstrap-index worker can drive a concurrent write
// that exceeds its deadline, and the timeouts dead-letter recoverable work. Wrap
// bounds in-flight writes so a slow backend holds its permits longer, which
// blocks additional workers at the write boundary and slows intake (closed-loop
// backpressure) instead of converting transient slowness into a dead-letter
// flood. The bound is opt-in and not a serialization fix: the ceiling is
// configurable and greater than one, so useful write concurrency is preserved.
//
// bootstrap-index (issue #4515, Lane B) wires the gate around the inner
// GroupExecutor-capable layer of its canonical NornicDB executor, not around
// its outer PhaseGroupExecutor entry point, because that outer call fans out
// into multiple concurrent inner GroupExecutor calls; gating only the outer
// call would leave the fan-out unbounded. See
// cmd/bootstrap-index/nornicdb_wiring.go and this package's README for detail.
//
// The package exists on its own (rather than in internal/runtime) to avoid an
// import cycle: the cypher package's internal tests import internal/runtime, so
// the observer adapter, which must import both cypher and telemetry, cannot live
// in runtime. Only the cmd layer consumes this package.
package graphbackpressure
