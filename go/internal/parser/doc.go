// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parser owns parser dispatch, registry lookup, tree-sitter runtime
// caching, and repository pre-scan orchestration. Optional SCIP protobuf
// index reduction lives in the internal/parser/scip subpackage, consumed
// directly by internal/collector/repo/git rather than through this package.
//
// Language subpackages own parse and pre-scan behavior behind thin parent
// wrappers or registered LanguageProvider implementations. This package owns
// the shared contract: path lookup, adapter dispatch, payload metadata
// attachment, deterministic import-map merging, Go package semantic pre-scan
// routing, and native Go stable symbol emission when module identity is
// known. Exact-name
// dispatch includes package-manager dependency files such as Cargo.toml,
// Cargo.lock, Package.resolved, and mix.lock when the adapter owns their evidence
// contract. Parser output feeds content shaping and durable facts, so parser
// changes must move fixtures, fact contracts, and downstream docs in lockstep.
// Options.EmitDataflow is the opt-in value-flow gate; durable
// dataflow_summaries rows require stable repository and package identity so
// FunctionID values remain generation-independent and persistence-safe.
//
// No-Regression Evidence: LanguageProvider dispatch is provider-first but
// preserves legacy built-in adapters when a definition has no provider, so
// existing parser output remains unchanged.
//
// No-Observability-Change: LanguageProvider dispatch adds no runtime signal;
// provider implementations continue to be diagnosed through the same
// collector parse-stage logs and file parse duration metric.
package parser
