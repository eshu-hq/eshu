// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package python extracts Python parser evidence behind the parent engine's
// Python dispatch methods.
//
// Parse reads .py and .ipynb inputs, runs tree-sitter with a caller-owned parser,
// and returns the payload buckets consumed by source collection and query truth:
// declarations, imports, calls, annotations, framework metadata, structural
// shell-command call-site evidence, and dead-code root hints, including cached
// properties, module dunder hooks, and nested dunder protocol hooks with
// same-scope assignment evidence. PreScan uses a declaration-only AST name pass
// for import-map discovery while preserving notebook code-cell extraction and
// module-name behavior. NotebookSource preserves the notebook code-cell
// invariant so notebook parsing cannot index markdown, raw cells, or partial
// JSON.
//
// When Options.EmitDataflow is set, Parse also emits the opt-in value-flow
// buckets "dataflow_functions", "taint_findings", and "interproc_findings"
// (built by cfg_emit.go over the python/pydataflow lowering and the shared
// internal/parser/dataflowemit renderer). The gate is off by default and the
// payload is byte-identical to before this feature when off. Shell-command
// evidence records only API and source location metadata; command text,
// arguments, and environment values are intentionally omitted.
package python
