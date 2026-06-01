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
package parser
