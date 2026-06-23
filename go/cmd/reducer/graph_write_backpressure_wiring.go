package main

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reducerGraphWriteGate is the single shared in-flight permit pool that bounds
// every reducer graph write under ESHU_GRAPH_WRITE_MAX_IN_FLIGHT (#3560, #3652).
// One gate is built per reducer service and applied as the OUTERMOST layer of
// each write path so a permit covers the whole inner attempt (timeout, retry,
// backend write):
//
//   - canonical / handler-edge / shared-projection / secrets-IAM / orphan-sweep
//     writers derive from the gate-wrapped base Executor (boundExecutor);
//   - the semantic entity path acquires a permit OUTSIDE its per-statement
//     TimeoutExecutor so permit-wait never counts against
//     ESHU_CANONICAL_WRITE_TIMEOUT (boundExecutor again, applied after the
//     timeout adapter — see buildReducerService);
//   - the workload / infrastructure-platform materializers, which write through
//     the separate reducer.CypherExecutor adapter, derive from the gate-wrapped
//     CypherExecutor (boundCypherExecutor).
//
// A non-positive ESHU_GRAPH_WRITE_MAX_IN_FLIGHT yields a nil gate, in which case
// every wrap method returns its inner executor unchanged (passthrough), so the
// gate is a safe no-op until an operator opts in. The ceiling is configurable and
// greater than one, so this is a bound, not serialization.
type reducerGraphWriteGate struct {
	gate *sourcecypher.BackpressureGate
}

// newReducerGraphWriteGate builds the shared permit pool from the environment
// knob. The returned gate is nil-tolerant: a disabled bound makes every wrap
// method a passthrough.
func newReducerGraphWriteGate(getenv func(string) string, instruments *telemetry.Instruments) reducerGraphWriteGate {
	return reducerGraphWriteGate{
		gate: graphbackpressure.NewGate(graphbackpressure.MaxInFlight(getenv), instruments),
	}
}

// boundExecutor wraps an Executor so it draws from the shared permit pool. The
// permit is acquired outermost, so apply this AFTER any timeout/retry adapter
// that must run inside the permit hold. A disabled gate returns inner unchanged.
func (g reducerGraphWriteGate) boundExecutor(inner sourcecypher.Executor) sourcecypher.Executor {
	return graphbackpressure.WrapExecutorWithGate(inner, g.gate)
}

// boundCypherExecutor wraps the materializer CypherExecutor path so workload and
// infrastructure-platform materializer writes draw from the same shared pool as
// canonical and semantic writes (#3652 P2). A disabled gate returns inner
// unchanged.
func (g reducerGraphWriteGate) boundCypherExecutor(inner reducer.CypherExecutor) reducer.CypherExecutor {
	return graphbackpressure.WrapCypherExecutorWithGate(inner, g.gate)
}

// boundSemanticEntityExecutor composes the semantic write path so the shared
// permit gate sits OUTSIDE the per-statement TimeoutExecutor
// (ESHU_CANONICAL_WRITE_TIMEOUT). It builds the timeout/ExecuteOnly adapter from
// rawExecutor (the unbounded base) so the timeout wraps the backend write only,
// then applies the gate as the outermost layer. Permit-wait therefore stays
// OUTSIDE the write timeout: a saturated pool delays a queued semantic write but
// never times it out (#3652 P1). The gate is the same shared pool as every other
// writer, so the bound stays unified.
func (g reducerGraphWriteGate) boundSemanticEntityExecutor(
	rawExecutor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	nornicDBTimeout time.Duration,
	nornicDBGroupedWrites bool,
) sourcecypher.Executor {
	return g.boundExecutor(
		semanticEntityExecutorForGraphBackend(
			rawExecutor,
			graphBackend,
			nornicDBTimeout,
			nornicDBGroupedWrites,
		),
	)
}
