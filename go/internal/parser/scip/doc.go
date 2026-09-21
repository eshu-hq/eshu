// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scip parses SCIP (SCIP Code Intelligence Protocol) index files and
// runs the external scip-* indexer CLIs that produce them, without depending
// on the parent parser dispatch package.
//
// IndexParser.Parse reads one index.scip protobuf file (produced by an
// external scip-* indexer such as scip-python or scip-go) and decodes it into
// the same file-payload shape the parent parser and content materializer
// expect: functions, classes, and variables keyed by absolute file path, plus
// a call-edge bucket (function_calls_scip) built by resolving each non-local
// occurrence's enclosing definition. A per-index SymbolTable maps every SCIP
// symbol string to its defining file, line, display name, and documentation,
// enriched from both per-document and external symbol declarations.
//
// Indexer runs the language-appropriate scip-* binary (LookPath/RunCommand are
// overridable for tests) and returns the index.scip path it produced.
// DetectProjectLanguage and DetectProjectLanguageGroups pick which SCIP-capable
// language(s) a candidate file set belongs to, using extensionConfigs for the
// extension-to-language-to-binary mapping and languagePriority to break ties
// deterministically when a repository mixes multiple SCIP-capable languages.
//
// This package holds no tree-sitter grammar and no AST walk: a SCIP index
// already carries resolved symbols and occurrences, so extraction here is
// projection over the decoded protobuf, not source parsing. It cannot compute
// real cyclomatic complexity (SCIP carries no statement-level AST), so
// function/method definitions always emit cyclomatic_complexity: 0 (unknown)
// rather than a fabricated constant; see issue #3488.
//
// The package's only dependency inside internal/parser is
// internal/parser/shared, for the payload-bucket-append helper it mirrors
// locally (bucket.go) to avoid importing the parent parser package. Production
// code and package-local tests must not import internal/parser.
//
// Definition payloads retain their source SCIP symbol string verbatim, so
// downstream reducer indexes can resolve cross-repository calls by
// source-backed symbol identity rather than by generation-bearing storage IDs.
// go/internal/reducer depends on that survival; its round-trip proof is
// parsed_file_data_contract_test.go in this directory.
//
// No-Regression Evidence: SCIP parsing runs only when collector configuration
// explicitly enables it, an allowed language group is present, and the matching
// external scip-* binary is on PATH. It supplements native parser output rather
// than replacing it; selected files absent from an index.scip document set
// still reach full coverage through the native parser path, so a missing
// indexer degrades enrichment, never file coverage.
//
// No-Observability-Change: this package emits no metric, span, status field,
// or runtime knob of its own. It is diagnosed through the collector snapshot
// parse-stage logs and the existing file parse duration metric that already
// cover the calling path.
package scip
