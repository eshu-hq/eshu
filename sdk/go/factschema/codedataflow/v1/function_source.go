// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// FunctionSource is the schema-version-1 typed payload for the
// "code_function_source" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_function_source fact is the git collector's per-source emission
// (go/internal/collector/git_snapshot_function_source.go
// functionSourceFactEnvelope) for one function parameter that is a value-flow
// taint entry point (for example an *http.Request argument), read from the
// parser's dataflow_sources bucket. The reducer's postgres loader
// (go/internal/storage/postgres/code_function_source_loader.go
// LoadCodeFunctionSources) persists these as interproc.Source entry ports so
// the cross-repo fixpoint has the source ports summary.Effects alone does not
// carry.
//
// FunctionID and Kind are REQUIRED: LoadCodeFunctionSources already drops any
// row missing either (`if id == "" || kind == "" { continue }`) — promoting
// them to required here makes that drop an explicit, operator-visible
// input_invalid dead-letter instead of a silent skip. ParamIndex and Language
// stay optional: ParamIndex is always present on the wire but a present zero
// (parameter index 0, the common "first argument" case) must stay
// indistinguishable from a genuine index-0 source, so it is not promoted to
// required-but-checked; Language is written only when the parser reported
// one.
type FunctionSource struct {
	// FunctionID is the function's durable, generation-independent identity
	// (repo\x1fpkg\x1freceiver\x1fname). Required: LoadCodeFunctionSources's
	// existing "id == ''" drop guard, made explicit and dead-lettering.
	FunctionID string `json:"function_id"`

	// Kind is the source classification (for example "http_request").
	// Required: LoadCodeFunctionSources's existing "kind == ''" drop guard,
	// made explicit and dead-lettering.
	Kind string `json:"kind"`

	// ParamIndex is the 0-based parameter index that is a taint entry point.
	// Optional (pointer) despite functionSourceFactEnvelope always writing
	// this key: a present-but-zero index is the legitimate "first parameter is
	// the source" case, and must decode identically to an absent value would
	// under the pre-Contract-System int coercion (both read as 0), so keeping
	// it a pointer here only distinguishes "explicit null" (malformed) from
	// "present" — it does not change the effective value read downstream.
	ParamIndex *int `json:"param_index,omitempty"`

	// Language is the parser-detected source language. Optional: written only
	// when non-empty.
	Language *string `json:"language,omitempty"`
}
