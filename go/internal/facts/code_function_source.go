// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeFunctionSourceFactKind identifies one function's param-level value-flow
// taint source (a parameter that is a taint entry point, e.g. an *http.Request
// argument) emitted by the collector from the parser's dataflow_sources bucket.
// The reducer persists these to the function-source store so the interprocedural
// fixpoint has the entry points it needs as source ports — the per-file analysis
// derives them from the AST but summary.Effects does not carry them. Absent when
// the value-flow gate is off, so the snapshot is byte-identical when value-flow
// emission is disabled.
const CodeFunctionSourceFactKind = "code_function_source"
