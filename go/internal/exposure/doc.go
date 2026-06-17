// Package exposure holds the curated, closed-vocabulary catalogs and (in later
// slices) the bounded path tracer for Eshu's code-to-cloud reachability taint
// capability (epic #2704, Level 1).
//
// The capability answers one differentiating question: is untrusted input
// reaching a cloud-exposed or privileged sink? Unlike code-only taint tools
// whose sink is always an AST node, an Eshu sink can be a correlated cloud
// fact — an IAM action, a reachable secret, an internet-exposed endpoint.
//
// This package is the declarative half of that capability. It does not read the
// graph, run Cypher, or write nodes. It declares:
//
//   - The cloud-sink catalog (sink_catalog.go): the closed set of sink kinds and
//     the graph relationship + target label that qualifies a node as reaching
//     each kind. SinkCatalogVersion content-hashes the catalog so a curated edit
//     trips downstream re-evaluation (the taintModelVersion discipline).
//
// Recognition is conservative by construction: a sink is only one of the closed
// SinkKind values, recognized only by a declared, provenance-cited edge. A sink
// kind that has no materialized graph fact yet (shell-exec) is kept in the
// vocabulary but marked non-graph-backed, so the tracer reports it unresolved
// rather than fabricating a match. This honesty contract — never invent a path —
// is the reason the catalog is data, not heuristics.
//
// The catalogs are package-level values built once; callers receive defensive
// copies and MUST NOT mutate the originals.
package exposure
