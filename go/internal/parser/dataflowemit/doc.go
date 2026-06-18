// Package dataflowemit renders language-neutral value-flow facts into the
// deterministic parser payload buckets the reducer consumes: "dataflow_functions"
// (per-function control-flow graphs and reaching-definition def->use edges),
// "taint_findings" (intraprocedural source-to-sink findings with confidence and
// provenance), "interproc_findings" (cross-function findings), and
// "dataflow_summaries" (each function's structural value-flow Effects — the TITO
// param/source flows the cross-repo composition fixpoint reloads and composes),
// and "dataflow_sources" (each function's param-level taint entry points, which
// the fixpoint needs as source ports and which the summaries do not carry).
// "dataflow_catalog_versions" carries freshness-only catalog content hashes.
//
// Each row carries a lang label so a downstream consumer can distinguish Go,
// TypeScript/JavaScript, and Python facts that share one schema. The renderers
// operate on the language-neutral cfg.Function, taint.Finding, interproc.Finding,
// interproc.Source, and summary.Effects types, so every language adapter emits an
// identical bucket shape. SortFunctionRows, SortFindingRows, SortSummaryRows, and
// SortSourceRows make the buckets byte-stable across runs; optional fields (class_context, sink_label,
// source_label, neutralized, cloud, and empty effect lists) are omitted when
// empty so the rows stay minimal.
package dataflowemit
