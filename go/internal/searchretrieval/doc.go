// Package searchretrieval defines the bounded internal retrieval contract for
// semantic evaluation.
//
// The package validates query scope, limit, timeout, and search mode before any
// backend adapter can run. It normalizes ranked EshuSearchDocument candidates
// into deterministic top-K responses that preserve derived truth labels,
// freshness, graph handles, truncation, and false-canonical-claim counts. It
// performs no Postgres, graph, NornicDB, HTTP, or MCP I/O.
package searchretrieval
