package main

import (
	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// boundReducerGraphWrites wraps rawExecutor with the shared graph-write
// backpressure bound so every reducer graph writer derived from it draws from a
// single in-flight permit pool (#3560, #3652 P2). It MUST be applied to the base
// executor before any writer (handler edges, canonical writers, shared
// projection, secrets/IAM, orphan sweep, workload materializers, semantic
// entities) is constructed; wrapping only the semantic executor left every other
// writer unbounded, so a slow backend still received unbounded concurrent
// reducer writes and dead-lettered recoverable work.
//
// A non-positive ESHU_GRAPH_WRITE_MAX_IN_FLIGHT returns rawExecutor unchanged
// (passthrough), preserving any interface it exposes (GroupExecutor), so the
// helper is a safe no-op until an operator opts in.
func boundReducerGraphWrites(
	rawExecutor sourcecypher.Executor,
	getenv func(string) string,
	instruments *telemetry.Instruments,
) sourcecypher.Executor {
	return graphbackpressure.Wrap(rawExecutor, graphbackpressure.MaxInFlight(getenv), instruments)
}
