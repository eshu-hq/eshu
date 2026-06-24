// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parser owns parser dispatch, registry lookup, tree-sitter runtime
// caching, repository pre-scan orchestration, and optional SCIP reduction.
//
// Language subpackages own parse and pre-scan behavior behind thin parent
// wrappers or registered LanguageProvider implementations. This package owns
// the shared contract: path lookup, adapter dispatch, payload metadata
// attachment, deterministic import-map merging, Go package semantic pre-scan
// routing, native Go stable symbol emission when module identity is known, and
// SCIP protobuf parsing. Exact-name
// dispatch includes package-manager dependency files such as Cargo.toml,
// Cargo.lock, Package.resolved, and mix.lock when the adapter owns their evidence
// contract. Parser output feeds content shaping and durable facts, so parser
// changes must move fixtures, fact contracts, and downstream docs in lockstep.
// SCIP definition payloads retain their source symbol string so downstream
// reducer indexes can resolve cross-repository calls by source-backed symbol
// identity instead of generation-bearing storage IDs.
// Options.EmitDataflow is the opt-in value-flow gate; durable
// dataflow_summaries rows require stable repository and package identity so
// FunctionID values remain generation-independent and persistence-safe.
//
// No-Regression Evidence: SCIP protobuf parsing is enabled by default through
// collector configuration only when an allowed language group and its external
// scip-* binary are available. It supplements native parser output; selected
// files that are absent from an index.scip document set still rely on the
// native parser path for complete file coverage.
// LanguageProvider dispatch is provider-first but preserves legacy built-in
// adapters when a definition has no provider, so existing parser output remains
// unchanged.
//
// No-Observability-Change: SCIP parsing uses the existing collector snapshot
// parse stage logs and file parse metrics; no parser metric, span, status
// field, or runtime knob changes are required for this completeness guard.
// LanguageProvider dispatch adds no runtime signal; provider implementations
// continue to be diagnosed through the same collector parse-stage logs and
// file parse duration metric.
package parser
