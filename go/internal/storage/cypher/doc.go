// Package cypher owns backend-neutral Cypher write contracts, canonical
// node and edge writers, statement metadata, and write instrumentation for
// Eshu's canonical graph.
//
// Writers in this package emit Statements that any supported graph backend
// can run through the Executor seam (InstrumentedExecutor, RetryingExecutor,
// TimeoutExecutor, ExecuteOnlyExecutor). Dialect-specific behavior must stay
// narrow and explicit: schema adapters, writer options, and the BuildCanonical*
// statement builders own backend differences so callers do not need to branch
// on ESHU_GRAPH_BACKEND. Writes must be idempotent and retry-safe; the
// canonical writers (CanonicalNodeWriter, EdgeWriter) are the boundary where
// node and edge invariants are enforced before bytes reach Neo4j or NornicDB.
package cypher
