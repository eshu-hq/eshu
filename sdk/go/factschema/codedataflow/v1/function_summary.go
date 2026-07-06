// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// FunctionSummary is the schema-version-1 typed payload for the
// "code_function_summary" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_function_summary fact is the git collector's once-per-function
// emission of a function's durable value-flow Effects
// (go/internal/collector/git_snapshot_function_summary.go
// functionSummaryFactEnvelope), read from the parser's dataflow_summaries
// bucket. The reducer's postgres loader
// (go/internal/storage/postgres/code_function_summary_loader.go
// codeFunctionSummaryEffectsFromPayload) reconstructs summary.Effects from
// this payload, keyed by FunctionID, so the interprocedural fixpoint
// (go/internal/reducer/code_function_summary_materialization.go) can persist
// and reload prior summaries across generations.
//
// FunctionID is REQUIRED: it is the durable, generation-independent identity
// every read site keys on (the summary store's map key, and
// durableFunctionRepo's repo-prefix parse). A code_function_summary fact
// missing it cannot be attributed to any function and MUST dead-letter, not
// silently vanish from LoadCodeFunctionSummaryEffects's `if id == ""` guard.
// Every other field is optional: GraphUID/Language/the four effect lists are
// all written conditionally by functionSummaryFactEnvelope (only non-empty
// lists are set), so a function with no observed effects in a given category
// legitimately omits that key.
type FunctionSummary struct {
	// FunctionID is the function's durable, generation-independent identity
	// (repo\x1fpkg\x1freceiver\x1fname, summary.FunctionID). Required: the
	// map key every reducer read site (function-summary store, source-repo
	// grouping via durableFunctionRepo, graph-id store) keys on.
	FunctionID string `json:"function_id"`

	// GraphUID is the graph Function entity uid this function's summary
	// resolves to, when the collector's per-repo resolver found one.
	// Optional: written only when non-empty
	// (`if summary.GraphUID != "" { payload["graph_uid"] = ... }`); a summary
	// whose function did not materialize as an entity is still persisted (the
	// FunctionID is durable) but has no graph_uid.
	GraphUID *string `json:"graph_uid,omitempty"`

	// Language is the parser-detected source language. Optional: written only
	// when non-empty.
	Language *string `json:"language,omitempty"`

	// ParamToReturn lists 0-based parameter indices that flow to the
	// function's return value. Optional: written only when non-empty.
	ParamToReturn []int `json:"param_to_return,omitempty"`

	// ParamToSink lists parameters that flow to a sink within the function.
	// Optional: written only when non-empty.
	ParamToSink []ParamSink `json:"param_to_sink,omitempty"`

	// SourceToReturn lists internal source kinds that flow to the function's
	// return value. Optional: written only when non-empty.
	SourceToReturn []string `json:"source_to_return,omitempty"`

	// ParamToCallArg lists parameter flows into a callee's argument
	// (through-into-through-out, TITO). Optional: written only when
	// non-empty.
	ParamToCallArg []CallArgFlow `json:"param_to_call_arg,omitempty"`
}

// ParamSink is one parameter-to-sink structural flow, mirroring
// go/internal/parser/summary.ParamSink. Both fields are required within the
// closed sub-struct: the collector never emits a param_to_sink entry with
// either field absent (it copies the parser's own fully-populated row
// verbatim).
type ParamSink struct {
	// Param is the 0-based parameter index that flows to the sink.
	Param int `json:"param"`
	// SinkKind is the sink classification this parameter flows to.
	SinkKind string `json:"sink_kind"`
}

// CallArgFlow is one parameter-to-callee-argument structural flow, mirroring
// go/internal/parser/summary.CallArgFlow. All three fields are required
// within the closed sub-struct, matching ParamSink's rationale.
type CallArgFlow struct {
	// Callee is the durable FunctionID of the function this flow calls into.
	Callee string `json:"callee"`
	// Param is the 0-based parameter index of the caller that flows into the
	// callee's argument.
	Param int `json:"param"`
	// Arg is the 0-based argument index of the callee this flow reaches.
	Arg int `json:"arg"`
}
