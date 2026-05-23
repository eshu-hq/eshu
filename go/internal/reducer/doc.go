// Package reducer owns Eshu's cross-domain materialization, shared projection,
// queued repair, and reducer-owned fact publication.
//
// Reducer handlers admit candidates from committed facts, build canonical graph
// rows or reducer fact rows, publish graph-readiness phases, and preserve
// idempotency across retries and replays. They do not call graph drivers
// directly; canonical graph writes go through storage/cypher, and durable fact
// writes go through narrow writer interfaces wired by cmd/reducer.
//
// Changes in this package must preserve the evidence path from raw facts to
// admitted candidate, projected row, graph or fact write, and API/MCP query
// truth. Queue ordering, generation supersession, phase publication, repair
// flows, shared projection readiness, and truth-emitting domain registration
// are package-level contracts.
package reducer
