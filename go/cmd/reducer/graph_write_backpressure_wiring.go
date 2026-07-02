// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reducerGraphWriteGate holds two INDEPENDENT in-flight permit pools that
// bound reducer graph writes: canonicalGate for the canonical write class,
// semanticGate for the semantic entity write class (issue #4448). #3652
// originally put every writer behind ONE shared pool; that caused head-of-line
// blocking because a slow write on one class could exhaust the shared permits
// and starve the other class even when that other class's own workload never
// saturated the pool. Splitting by class removes that coupling while
// preserving the same closed-loop backpressure behavior per class.
//
// Each gate is applied as the OUTERMOST layer of its write paths so a permit
// covers the whole inner attempt (timeout, retry, backend write):
//
//   - canonical / handler-edge / shared-projection / secrets-IAM / orphan-sweep
//     writers derive from the canonical-gate-wrapped base Executor
//     (boundExecutor);
//   - the workload / infrastructure-platform materializers, which write through
//     the separate reducer.CypherExecutor adapter, derive from the
//     canonical-gate-wrapped CypherExecutor (boundCypherExecutor) — they share
//     the canonical pool with the writers above, matching #3652's original
//     "materializer writes must not bypass the bound" intent, just scoped to
//     the canonical class;
//   - the semantic entity path acquires a SEMANTIC permit OUTSIDE its
//     per-statement TimeoutExecutor so permit-wait never counts against
//     ESHU_CANONICAL_WRITE_TIMEOUT (boundSemanticEntityExecutor, applied after
//     the timeout adapter — see buildReducerService); this permit is drawn from
//     semanticGate, never canonicalGate.
//
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT and
// ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT size each gate independently; either
// unset falls back to the legacy ESHU_GRAPH_WRITE_MAX_IN_FLIGHT so an operator
// who only configured the old shared knob keeps an identical bound on both
// classes until they opt into per-class tuning. A non-positive ceiling (after
// fallback) yields a nil gate for that class, in which case every wrap method
// on that class returns its inner executor unchanged (passthrough), so each
// gate is independently a safe no-op until an operator opts in. Ceilings are
// configurable and greater than one, so this is a bound, not serialization.
type reducerGraphWriteGate struct {
	canonicalGate *sourcecypher.BackpressureGate
	semanticGate  *sourcecypher.BackpressureGate
}

// newReducerGraphWriteGate builds the two independent permit pools from the
// environment knobs. Both returned gates are nil-tolerant: a disabled bound
// makes every wrap method for that class a passthrough.
func newReducerGraphWriteGate(getenv func(string) string, instruments *telemetry.Instruments) reducerGraphWriteGate {
	return reducerGraphWriteGate{
		canonicalGate: graphbackpressure.NewGate(
			graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.CanonicalMaxInFlightEnv),
			instruments,
			graphbackpressure.CanonicalGateName,
		),
		semanticGate: graphbackpressure.NewGate(
			graphbackpressure.ClassMaxInFlight(getenv, graphbackpressure.SemanticMaxInFlightEnv),
			instruments,
			graphbackpressure.SemanticGateName,
		),
	}
}

// boundExecutor wraps an Executor so it draws from the canonical permit pool.
// The permit is acquired outermost, so apply this AFTER any timeout/retry
// adapter that must run inside the permit hold. A disabled gate returns inner
// unchanged.
func (g reducerGraphWriteGate) boundExecutor(inner sourcecypher.Executor) sourcecypher.Executor {
	return graphbackpressure.WrapExecutorWithGate(inner, g.canonicalGate)
}

// boundCypherExecutor wraps the materializer CypherExecutor path so workload
// and infrastructure-platform materializer writes draw from the same canonical
// pool as the other canonical-class writers (#3652 P2, scoped to the canonical
// gate by #4448). A disabled gate returns inner unchanged.
func (g reducerGraphWriteGate) boundCypherExecutor(inner reducer.CypherExecutor) reducer.CypherExecutor {
	return graphbackpressure.WrapCypherExecutorWithGate(inner, g.canonicalGate)
}

// boundSemanticEntityExecutor composes the semantic write path so the SEMANTIC
// permit gate sits OUTSIDE the per-statement TimeoutExecutor
// (ESHU_CANONICAL_WRITE_TIMEOUT). It builds the timeout/ExecuteOnly adapter from
// rawExecutor (the unbounded base) so the timeout wraps the backend write only,
// then applies the semantic gate as the outermost layer. Permit-wait therefore
// stays OUTSIDE the write timeout: a saturated semantic pool delays a queued
// semantic write but never times it out (#3652 P1). The permit is drawn from
// semanticGate, an INDEPENDENT pool from canonicalGate, so a saturated semantic
// pool cannot starve canonical writers and vice versa (#4448).
func (g reducerGraphWriteGate) boundSemanticEntityExecutor(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	nornicDBTimeout time.Duration,
	nornicDBGroupedWrites bool,
) sourcecypher.Executor {
	inner := semanticEntityExecutorForGraphBackend(
		rawExecutor,
		graphBackend,
		nornicDBTimeout,
		nornicDBGroupedWrites,
	)
	return graphbackpressure.WrapExecutorWithGate(inner, g.semanticGate)
}
