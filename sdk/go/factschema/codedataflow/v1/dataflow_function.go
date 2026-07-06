// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DataflowFunction is the schema-version-1 typed payload for the
// "code_dataflow_function" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_dataflow_function fact is the git collector's per-function emission
// of exact parser evidence (go/internal/collector/git_snapshot_dataflow_function.go
// dataflowFunctionFactEnvelope): one function's bounded CFG, reaching-
// definition, and control-dependence facts, read from the parser's
// dataflow_functions bucket. Unlike the other five kinds in this family, no
// reducer materialization handler decodes this kind today — its only
// consumer is the query layer's code-flow read model
// (go/internal/query/code_flow_postgres.go codeFlowFunctionFromPayload),
// which reads the payload raw via StringVal/IntVal/anySliceVal/mapSliceVal
// helpers, not through this contracts seam. This struct exists for the
// family's manifest/schema completeness (every fact kind the family emits
// gets a typed struct), not because a reducer read site needs it decoded;
// see the package doc.go for the family's scope.
//
// RepoID and RelativePath are REQUIRED, mirroring codegraphv1.File's join
// identity (a function-level dataflow record with no repository or file to
// attribute it to is not a meaningful record), even though no reducer
// consumer enforces this today — the requirement documents the intended
// identity contract for the day a reducer handler does decode this kind.
// Every other field is optional: written conditionally by
// dataflowFunctionFactEnvelope, matching TaintEvidence/InterprocEvidence's
// precedent for the same nested-evidence shapes.
type DataflowFunction struct {
	// RepoID is the owning repository's canonical id. Required: the join key
	// that attributes this dataflow record to a repository.
	RepoID string `json:"repo_id"`

	// RelativePath is the file's path relative to its repository root.
	// Required: combined with RepoID and FunctionName it forms this record's
	// identity.
	RelativePath string `json:"relative_path"`

	// FunctionName is the function's name. Required: dataflowFunctionFactEnvelope
	// never builds a record for a function with a blank name
	// (buildDataflowFunctions skips it before emission).
	FunctionName string `json:"function_name"`

	// FunctionUID is the graph Function entity uid this record resolves to,
	// when the collector's per-repo resolver found one. Optional: written
	// only when non-empty.
	FunctionUID *string `json:"function_uid,omitempty"`

	// Language is the parser-detected source language. Optional: written only
	// when non-empty.
	Language *string `json:"language,omitempty"`

	// LineNumber is the function's 1-based starting line number. Optional:
	// written only when greater than zero.
	LineNumber *int `json:"line_number,omitempty"`

	// CFGBlocks is the function's control-flow-graph block list. Each element
	// is a map (a "block" record, e.g. {"id":..,"succs":[..]}) — never a bare
	// scalar — so this is []map[string]any rather than []any: an open []any
	// generates an unconstrained "items: true" JSON Schema, a construct the
	// collector conformance validator's supported schema subset rejects
	// (sdk/go/collector/conformance), unlike []map[string]any's constrained
	// "items: object" shape. The inner map keys stay UNTYPED (read-and-forward,
	// mirroring codegraphv1.File.ParsedFileData's opacity precedent) — only
	// the outer "each element is an object" shape is asserted here. Optional:
	// written only when non-empty.
	CFGBlocks []map[string]any `json:"cfg_blocks,omitempty"`

	// CFGEdges is the function's control-flow-graph edge list. Each element is
	// always a map ({"from":..,"to":..}, dataflowCFGEdges), so this is
	// []map[string]any for the same conformance-schema reason as CFGBlocks.
	// Optional: written only when non-empty.
	CFGEdges []map[string]any `json:"cfg_edges,omitempty"`

	// DefUse is the function's reaching-definition/def-use record list, left
	// UNTYPED for the same reason as CFGBlocks. Optional: written only when
	// non-empty.
	DefUse []map[string]any `json:"def_use,omitempty"`

	// ControlDependencies is the function's control-dependence record list,
	// left UNTYPED for the same reason as CFGBlocks. Optional: written only
	// when non-empty.
	ControlDependencies []map[string]any `json:"control_dependencies,omitempty"`

	// Overflow reports whether the parser hit an analysis bound (block count,
	// edge count, ...) while building this record. Optional: written only
	// when true.
	Overflow *bool `json:"overflow,omitempty"`

	// OverflowReason names which bound(s) were hit, when Overflow is true.
	// Optional: written only when non-empty.
	OverflowReason *string `json:"overflow_reason,omitempty"`
}
