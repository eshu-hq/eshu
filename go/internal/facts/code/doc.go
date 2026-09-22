// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package code declares the fact-kind constants for parser-emitted
// code-intelligence evidence: the value-flow scan marker and per-function
// dataflow record, the function-level taint source and summary facts the
// collector emits from the parser's dataflow buckets, and the resolved
// intraprocedural and interprocedural taint findings the collector emits
// for reducer graph projection.
//
// It moved out of go/internal/facts (issue #6776). Every exported
// identifier was destuttered per docs/internal/naming.md rule 4: the
// former CodeTaintEvidenceFactKind is now TaintEvidenceFactKind,
// CodeInterprocEvidenceFactKind is now InterprocEvidenceFactKind,
// CodeFunctionSourceFactKind is now FunctionSourceFactKind, and
// CodeFunctionSummaryFactKind is now FunctionSummaryFactKind.
// DataflowScannedFactKind and DataflowFunctionFactKind kept their names —
// they never stuttered. Every pre-move facts.Code* spelling still resolves
// through the facts root's transitional compat_code.go, which forwards all
// seven of them (including the CodeFlowReadFactKinds function) to these
// destuttered names; each compat entry is deleted only once its last caller
// has moved off it. New code should prefer the code.<Name> spelling
// directly.
//
// Unlike the cloud package, these kinds are NOT part of the facts root's
// schema-version admission regime: none of them appears in
// go/internal/facts/schema_version.go's schemaVersionFamilies table, and
// none is registered in specs/fact-kind-registry.v1.yaml (that file's
// "code" family is a distinct, admission-exempt entry for the legacy git
// file/repository kinds, not these). They carry no schema-version constant
// and classify as facts.CompatibilityUnknownKind under
// facts.ClassifySchemaVersion. That file's own comment names these kinds
// ("code_dataflow_*, taint, interproc, function_summary") as
// version-less by design, with the admission-exempt pattern available if
// they are registered later.
//
// FlowReadFactKinds() is the one cross-cutting accessor: it returns the
// ordered subset (TaintEvidenceFactKind, InterprocEvidenceFactKind,
// DataflowFunctionFactKind) that must stay in lockstep with the SQL
// literal in codemodel.ListActiveCodeFlowFactsSQL
// (go/internal/query/codemodel/code_flow_postgres.go) and the
// fact_records_code_flow_repo_idx partial index predicate. Adding a kind
// to this function without updating both SQL sites leaves the index
// silently short of a kind the read still queries.
//
// This is a leaf declaration package: string constants and one ordering
// accessor, no I/O, no collector or reducer logic. It has no _test.go
// files of its own; its constants are exercised through the collector,
// reducer, projector, and query/postgres packages that consume them.
package code
