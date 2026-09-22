// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// code-intelligence fact family, which moved to [code] in issue #6776 so the
// root directory drops back under the 40-file dirgate cap. Every entry is an
// alias or a thin forwarder with no behavior change: the value, the type
// identity, and the returned bytes are the same ones the root declared
// before the move.
//
// It carries only the names that still have a caller. Nothing in the facts
// root references this family -- these kinds are not in
// schemaVersionFamilies and not in specs/fact-kind-registry.v1.yaml -- so
// every entry here exists for a caller outside this package:
// collector/repo/git, which emits these facts, the reducer/code and
// projector/code families that consume them, and the query/codemodel,
// query/codequery and storage/postgres read path. A later family move adds a
// stanza to this file and never creates a new compat_*.go, and each entry
// here is deleted once its last caller has moved to the code package
// directly (see the importer-migration follow-up, #6950).

import "github.com/eshu-hq/eshu/go/internal/facts/code"

// Stanza: dataflow.go (moved to code/dataflow.go).
const (
	// CodeDataflowFunctionFactKind identifies one parser-emitted function-level
	// dataflow record carrying bounded CFG, reaching-definition, and control-
	// dependence facts. It is exact parser evidence for API/MCP code-flow
	// readbacks, not canonical graph truth. Absent when the value-flow gate is
	// off. See [code.DataflowFunctionFactKind].
	CodeDataflowFunctionFactKind = code.DataflowFunctionFactKind
	// CodeDataflowScannedFactKind marks one scope generation in which the value-
	// flow gate (ESHU_EMIT_DATAFLOW) ran, emitted once per generation regardless
	// of whether any taint or interproc findings were produced. It carries no
	// findings; it is a reconciliation signal so the reducer projects the value-
	// flow evidence domains — and therefore retracts stale evidence — even when
	// the current generation's finding set is empty. Absent when the gate is off,
	// so the snapshot is byte-identical when value-flow emission is disabled. See
	// [code.DataflowScannedFactKind].
	CodeDataflowScannedFactKind = code.DataflowScannedFactKind
)

// Stanza: flow_read_kinds.go (moved to code/flow_read_kinds.go).

// CodeFlowReadFactKinds returns the canonical, ordered set of fact kinds the
// cumulative-active code-flow read selects and its repo-anchored partial index
// covers. It is the single source of truth that keeps three sites in lockstep:
// - query.listActiveCodeFlowFactsSQL's literal `fact_kind IN (...)` conjunct,
// which unlocks the partial index under a generic prepared plan (#5280); - the
// fact_records_code_flow_repo_idx partial `WHERE fact_kind IN (...)`
// predicate, which only accelerates the kinds named in it; and -
// query.codeFlowFactKinds, whose union across every CodeFlowKind is exactly
// this set (the per-read $1 subset is always drawn from here). Adding a code-
// flow fact kind here forces the lockstep guard tests in the query and
// postgres packages to fail until both SQL sites cover it, so the index can
// never silently miss a kind the read queries (which would over-fetch that
// kind through the old all-scope heap filter while the write path still paid
// the index's maintenance cost). Returns a fresh slice so callers cannot
// mutate the canonical order. See [code.FlowReadFactKinds].
func CodeFlowReadFactKinds() []string {
	return code.FlowReadFactKinds()
}

// Stanza: function_source.go (moved to code/function_source.go).
const (
	// CodeFunctionSourceFactKind identifies one function's param-level value-flow
	// taint source (a parameter that is a taint entry point, e.g. an
	// *http.Request argument) emitted by the collector from the parser's
	// dataflow_sources bucket. The reducer persists these to the function-source
	// store so the interprocedural fixpoint has the entry points it needs as
	// source ports — the per-file analysis derives them from the AST but
	// summary.Effects does not carry them. Absent when the value-flow gate is
	// off, so the snapshot is byte-identical when value-flow emission is
	// disabled. See [code.FunctionSourceFactKind].
	CodeFunctionSourceFactKind = code.FunctionSourceFactKind
)

// Stanza: function_summary.go (moved to code/function_summary.go).
const (
	// CodeFunctionSummaryFactKind identifies one function's durable value-flow
	// summary (its structural Effects) emitted by the collector from the parser's
	// dataflow_summaries bucket. The reducer reconstructs the Effects and
	// persists them to the function-summary store, keyed by the generation-
	// independent FunctionID, so the interprocedural fixpoint can reload prior
	// summaries and recompose only changed callees across runs. It is summary
	// input, never canonical graph truth. Absent when the value-flow gate is off,
	// so the snapshot is byte-identical when value-flow emission is disabled. See
	// [code.FunctionSummaryFactKind].
	CodeFunctionSummaryFactKind = code.FunctionSummaryFactKind
)

// Stanza: interproc.go (moved to code/interproc.go).
const (
	// CodeInterprocEvidenceFactKind identifies one resolved cross-function value-
	// flow taint finding emitted by the collector for reducer graph projection.
	// The collector resolves both endpoints to their Function entity uids and
	// emits the finding as a fact of this kind; the reducer projects it as a
	// TAINT_FLOWS_TO edge from the source Function to the sink Function
	// (evidence, never canonical truth). See [code.InterprocEvidenceFactKind].
	CodeInterprocEvidenceFactKind = code.InterprocEvidenceFactKind
)

// Stanza: taint.go (moved to code/taint.go).
const (
	// CodeTaintEvidenceFactKind identifies one resolved intraprocedural value-
	// flow taint finding emitted by the collector for reducer graph projection.
	// The collector resolves each finding to its Function entity uid and emits
	// the finding as a fact of this kind; the reducer projects it as a
	// CodeTaintEvidence graph node attached to that Function (evidence, never
	// canonical truth). See [code.TaintEvidenceFactKind].
	CodeTaintEvidenceFactKind = code.TaintEvidenceFactKind
)
