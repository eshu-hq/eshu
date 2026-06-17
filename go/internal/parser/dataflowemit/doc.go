// Package dataflowemit renders language-neutral value-flow facts into the
// deterministic parser payload buckets the reducer consumes: "dataflow_functions"
// (per-function control-flow graphs and reaching-definition def->use edges),
// "taint_findings" (intraprocedural source-to-sink findings with confidence and
// provenance), and "interproc_findings" (cross-function findings).
//
// Each row carries a lang label so a downstream consumer can distinguish Go,
// TypeScript/JavaScript, and Python facts that share one schema. The renderers
// operate on the language-neutral cfg.Function, taint.Finding, and
// interproc.Finding types, so every language adapter emits an identical bucket
// shape. SortFunctionRows and SortFindingRows make the buckets byte-stable across
// runs; optional fields (class_context, sink_label, source_label, neutralized,
// cloud) are omitted when empty so the rows stay minimal.
package dataflowemit
