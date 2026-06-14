// Package parser owns parser dispatch, registry lookup, tree-sitter runtime
// caching, repository pre-scan orchestration, and optional SCIP reduction.
//
// Language subpackages own parse and pre-scan behavior behind thin parent
// wrappers. This package owns the shared contract: path lookup, adapter
// dispatch, payload metadata attachment, deterministic import-map merging, Go
// package semantic pre-scan routing, and SCIP protobuf parsing. Exact-name
// dispatch includes package-manager dependency files such as Cargo.toml,
// Cargo.lock, Package.resolved, and mix.lock when the adapter owns their evidence
// contract. Parser output feeds content shaping and durable facts, so parser
// changes must move fixtures, fact contracts, and downstream docs in lockstep.
//
// No-Regression Evidence: SCIP protobuf parsing remains opt-in through
// collector configuration and supplements native parser output; selected files
// that are absent from an index.scip document set still rely on the native
// parser path for complete file coverage.
//
// No-Observability-Change: SCIP parsing uses the existing collector snapshot
// parse stage logs and file parse metrics; no parser metric, span, status
// field, or runtime knob changes are required for this completeness guard.
package parser
