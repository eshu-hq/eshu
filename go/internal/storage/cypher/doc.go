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
// Code-call rows may materialize as CALLS or REFERENCES depending on parser
// semantics; Go and TypeScript type-reference metadata must remain REFERENCES
// so graph truth does not claim that type literals are invocations. When
// reducer evidence includes endpoint entity labels, EdgeWriter anchors code
// relationship writes on whitelisted exact labels such as Function, Class,
// File, Interface, Struct, and TypeAlias plus uid, and falls back to the older
// label-family shape only for legacy rows.
// Canonical entity retractions run after current entity upserts and keep
// concrete labels in the Cypher anchor so stale-node and stale-edge cleanup
// remains selective on supported graph backends. Stale File-to-entity CONTAINS
// cleanup is owned by entity retraction, not a separate per-file relationship
// filter. Repository writes clear current identity nodes in a dedicated cleanup
// phase before their MERGE phase. Directory and File nodes update in place
// because replacing them with DETACH DELETE is too expensive on local graph
// backends; missing nested files use a guarded MERGE through their parent
// Directory, while repository-root files use a Repository-contained path that
// does not require a synthetic root Directory. High-volume analysis metadata
// such as dead_code_root_kinds stays in the content store unless a graph query
// owns a proven need for that property.
package cypher
