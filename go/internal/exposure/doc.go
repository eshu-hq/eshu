// Package exposure holds the curated, closed-vocabulary catalogs and (in later
// slices) the bounded path tracer for Eshu's code-to-cloud reachability taint
// capability (epic #2704, Level 1).
//
// The capability answers one differentiating question: is untrusted input
// reaching a cloud-exposed or privileged sink? Unlike code-only taint tools
// whose sink is always an AST node, an Eshu sink can be a correlated cloud
// fact — an IAM action, a reachable secret, an internet-exposed endpoint, a
// resolved SQL table queried by a function, or a structural shell-command node
// reached through EXECUTES_SHELL. Config-security-key and IaC-misconfiguration
// sink kinds are cataloged as #3191 fixtures but intentionally non-graph-backed
// until a Function-anchored materializer and fixpoint loader path can prove
// them.
//
// This package is the declarative half of that capability. It does not read the
// graph, run Cypher, or write nodes. It declares:
//
//   - The cloud-sink catalog (sink_catalog.go): the closed set of sink kinds and
//     the graph relationship + target label that qualifies a node as reaching
//     each kind. SinkCatalogVersion content-hashes the catalog so a curated edit
//     trips downstream re-evaluation (the taintModelVersion discipline).
//
//   - The taint-source catalog (source_catalog.go): the closed set of
//     untrusted-input entry points, classified from the parser's existing
//     dead_code_root_kinds tokens (HTTP/RPC/Lambda handlers, message consumers,
//     CLI commands). ClassifySource maps a function's root-kind tokens to a
//     source kind; RankSourceExposure ranks exposure honestly, only labelling a
//     source internet_exposed when the tracer proves its endpoint reaches
//     0.0.0.0/0. Entrypoints, public API, tests, and generated code are
//     intentionally not sources.
//
//   - The exposure-path assembler (path_trace.go): the conservative truth-state
//     vocabulary (exact/partial/ambiguous/unresolved), the honest severity
//     combination (CombinePathSeverity), and BuildExposureFinding, which turns
//     plain bounded-traversal data into a finding. It is pure data in, data out
//     so the query handler can run the graph traversal and feed it candidates;
//     it never fabricates a path or severity and always labels findings derived.
//
// Recognition is conservative by construction: a sink is only one of the closed
// SinkKind values, recognized only by a declared, provenance-cited edge. A sink
// kind that has no materialized graph fact is kept in the vocabulary but marked
// non-graph-backed, so the tracer reports it unresolved rather than fabricating
// a match. This honesty contract — never invent a path — is the reason the
// catalog is data, not heuristics.
//
// The catalogs are package-level values built once; callers receive defensive
// copies and MUST NOT mutate the originals.
package exposure
