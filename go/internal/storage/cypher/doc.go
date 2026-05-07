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
// Canonical entity retractions run after current entity upserts and keep
// concrete labels in the Cypher anchor so stale-node and stale-edge cleanup
// remains selective on supported graph backends. Stale File-to-entity CONTAINS
// cleanup is owned by entity retraction, not a separate per-file relationship
// filter. Repository writes clear current identity nodes in a dedicated cleanup
// phase before their MERGE phase. Directory and File nodes update in place
// because replacing them with DETACH DELETE is too expensive on local graph
// backends; missing files use a guarded MERGE so existing File.path rows never
// hit the MERGE unique-conflict path.
package cypher
