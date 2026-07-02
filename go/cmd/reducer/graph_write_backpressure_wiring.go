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

// reducerGraphWriteGate holds two in-flight permit pools that bound reducer
// graph writes: canonicalGate for the canonical write class, semanticGate for
// the semantic entity write class (issue #4448). #3652 originally put every
// writer behind ONE shared pool; that caused head-of-line blocking because a
// slow write on one class could exhaust the shared permits and starve the
// other class even when that other class's own workload never saturated the
// pool. Splitting by class removes that coupling while preserving the same
// closed-loop backpressure behavior per class — but ONLY once an operator
// opts into per-class sizing.
//
// aggregateGate is the P1 fix for the legacy-only case: when neither
// ESHU_GRAPH_WRITE_CANONICAL_MAX_IN_FLIGHT nor
// ESHU_GRAPH_WRITE_SEMANTIC_MAX_IN_FLIGHT is set, ClassMaxInFlight falls back
// to ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=N independently for EACH class, which
// would let up to 2N writes run concurrently (N canonical + N semantic) — an
// unmeasured doubling of the concurrency budget an existing deployment sized
// to backend headroom. aggregateGate is a THIRD gate, sized to N, that both
// classes draw a permit from IN ADDITION to their own class gate whenever
// AnyClassMaxInFlightSet is false, so the combined total across both classes
// stays bounded by N, exactly reproducing the pre-#4448 shared-pool capacity.
// Once an operator sets either per-class env explicitly, aggregateGate is nil
// (AggregateMaxInFlight returns 0) and the two class gates are the sole,
// fully independent bounds — the opt-in #4448 fix.
//
// Each permit is acquired OUTERMOST-FIRST in a FIXED order (aggregate, then
// class) by every write path, so no lock-ordering cycle is possible between
// the two gate layers:
//
//   - canonical / handler-edge / shared-projection / secrets-IAM / orphan-sweep
//     writers derive from the aggregate-then-canonical-gate-wrapped base
//     Executor (boundExecutor);
//   - the workload / infrastructure-platform materializers, which write through
//     the separate reducer.CypherExecutor adapter, derive from the
//     aggregate-then-canonical-gate-wrapped CypherExecutor (boundCypherExecutor)
//     — they share the canonical pool with the writers above, matching #3652's
//     original "materializer writes must not bypass the bound" intent, just
//     scoped to the canonical class;
//   - the semantic entity path acquires its permits OUTSIDE its per-statement
//     TimeoutExecutor so permit-wait never counts against
//     ESHU_CANONICAL_WRITE_TIMEOUT (boundSemanticEntityExecutor, applied after
//     the timeout adapter — see buildReducerService); these permits are drawn
//     from aggregateGate then semanticGate, never canonicalGate.
//
// A non-positive ceiling yields a nil gate for that layer, in which case every
// wrap method touching it returns its inner executor unchanged (passthrough),
// so each layer is independently a safe no-op until an operator opts in.
// Ceilings are configurable and greater than one, so this is a bound, not
// serialization.
type reducerGraphWriteGate struct {
	aggregateGate *sourcecypher.BackpressureGate
	canonicalGate *sourcecypher.BackpressureGate
	semanticGate  *sourcecypher.BackpressureGate
}

// newReducerGraphWriteGate builds the permit pools from the environment
// knobs. All three returned gates are nil-tolerant: a disabled bound makes
// every wrap method touching it a passthrough. aggregateGate is non-nil only
// while neither per-class env is set (see AggregateMaxInFlight); it is the
// #4448 P1 guard that keeps the combined canonical+semantic total bounded by
// the legacy ESHU_GRAPH_WRITE_MAX_IN_FLIGHT ceiling for deployments that have
// not opted into per-class sizing.
func newReducerGraphWriteGate(getenv func(string) string, instruments *telemetry.Instruments) reducerGraphWriteGate {
	return reducerGraphWriteGate{
		aggregateGate: graphbackpressure.NewGate(
			graphbackpressure.AggregateMaxInFlight(getenv),
			instruments,
			graphbackpressure.AggregateGateName,
		),
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

// boundExecutor wraps an Executor so it draws an aggregate permit (legacy-only
// mode; passthrough once per-class sizing is configured) and then a canonical
// permit. Permits are acquired outermost-first, so apply this AFTER any
// timeout/retry adapter that must run inside the permit hold. A disabled gate
// at either layer is a passthrough for that layer.
func (g reducerGraphWriteGate) boundExecutor(inner sourcecypher.Executor) sourcecypher.Executor {
	aggregateBound := graphbackpressure.WrapExecutorWithGate(inner, g.aggregateGate)
	return graphbackpressure.WrapExecutorWithGate(aggregateBound, g.canonicalGate)
}

// boundCypherExecutor wraps the materializer CypherExecutor path so workload
// and infrastructure-platform materializer writes draw the same aggregate and
// canonical permits as the other canonical-class writers (#3652 P2, scoped to
// the canonical gate by #4448). A disabled gate at either layer is a
// passthrough for that layer.
func (g reducerGraphWriteGate) boundCypherExecutor(inner reducer.CypherExecutor) reducer.CypherExecutor {
	aggregateBound := graphbackpressure.WrapCypherExecutorWithGate(inner, g.aggregateGate)
	return graphbackpressure.WrapCypherExecutorWithGate(aggregateBound, g.canonicalGate)
}

// boundSemanticEntityExecutor composes the semantic write path so the permit
// gates sit OUTSIDE the per-statement TimeoutExecutor
// (ESHU_CANONICAL_WRITE_TIMEOUT). It builds the timeout/ExecuteOnly adapter
// from rawExecutor (the unbounded base) so the timeout wraps the backend write
// only, then applies the aggregate gate and the semantic gate, in that order,
// as the outermost layers. Permit-wait therefore stays OUTSIDE the write
// timeout: a saturated pool delays a queued semantic write but never times it
// out (#3652 P1). The class permit is drawn from semanticGate, independent
// from canonicalGate once per-class sizing is configured (#4448); while it is
// not, both classes also draw from the same aggregateGate, so the combined
// total stays bounded by the legacy ceiling (#4448 P1).
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
	aggregateBound := graphbackpressure.WrapExecutorWithGate(inner, g.aggregateGate)
	return graphbackpressure.WrapExecutorWithGate(aggregateBound, g.semanticGate)
}
