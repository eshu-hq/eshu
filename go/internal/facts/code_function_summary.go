// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeFunctionSummaryFactKind identifies one function's durable value-flow
// summary (its structural Effects) emitted by the collector from the parser's
// dataflow_summaries bucket. The reducer reconstructs the Effects and persists
// them to the function-summary store, keyed by the generation-independent
// FunctionID, so the interprocedural fixpoint can reload prior summaries and
// recompose only changed callees across runs. It is summary input, never
// canonical graph truth. Absent when the value-flow gate is off, so the snapshot
// is byte-identical when value-flow emission is disabled.
const CodeFunctionSummaryFactKind = "code_function_summary"
