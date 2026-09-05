// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file is the #6060 lane-A P0 shim: same-name forwarders and aliases for
// the root symbols whose canonical definitions moved to a leaf package in P0,
// so the code-family files that have not moved yet keep compiling with zero
// edits to their files. Staying files MUST NOT use these names -- they name
// the leaf package directly (see the P0 handoff) -- so each entry below is
// deleted together with the last family-file user when that family moves and
// qualifies its call sites. The file itself is deleted when it empties.
//
// The file is deliberately NOT named code_family_shim.go: the lane-A family is
// the root non-test ^(code|complexity|dead_code) set, and a code_-prefixed
// staying file would match a later phase's family glob and be swept into a
// move it must survive.
// deadCodeCandidateLabels aliases the leaf-owned candidate set so the
// code-family dead-code scan files keep their call sites unchanged. The
// staying openAPI contract test names querycontract directly.
var deadCodeCandidateLabels = querycontract.DeadCodeCandidateLabels

// importDependencyInternalScanLimit aliases the leaf-owned scan bound so the
// code-family dependency readers keep their call sites unchanged. The staying
// grant and queryplan tests name querycontract directly.
const importDependencyInternalScanLimit = querycontract.ImportDependencyInternalScanLimit

// callGraphMetricsRequest aliases the leaf-owned request type so the staying
// call-graph-metrics handler, data reader, and tests keep their signatures
// and literals unchanged. The type split to codemodel with its methods in L1
// (Go requires methods to live with their declaration); Validate and
// MetricType are the exported methods the staying handler and data reader
// call. Delete with code_call_graph_metrics.go's handler move.
type callGraphMetricsRequest = codemodel.CallGraphMetricsRequest

// callGraphMetricsStats aliases the leaf-owned scan stats so the staying
// data reader keeps its destructuring unchanged; only ExpandedNodes is
// exported (the staying span attribute reads it). Delete with
// code_call_graph_metrics.go's handler move.
type callGraphMetricsStats = codemodel.CallGraphMetricsStats

// callGraphMetricsMaxOffset aliases the leaf-owned page-window bound so the
// staying metrics tests keep binding through it. Delete with
// code_call_graph_metrics.go's handler move.
const callGraphMetricsMaxOffset = codemodel.CallGraphMetricsMaxOffset

// callGraphMetricsResponse forwards to the leaf-owned response shaper so
// the staying data reader and metrics tests keep their call sites
// unchanged. Delete with code_call_graph_metrics.go's handler move.
func callGraphMetricsResponse(req callGraphMetricsRequest, rows []map[string]any) map[string]any {
	return codemodel.CallGraphMetricsResponse(req, rows)
}

// callGraphMetricsRows forwards to the leaf-owned ranker so the staying
// metrics tests keep their call sites unchanged. Delete with
// code_call_graph_metrics.go's handler move.
func callGraphMetricsRows(req callGraphMetricsRequest, edgeRows []map[string]any) []map[string]any {
	return codemodel.CallGraphMetricsRows(req, edgeRows)
}

// callGraphMetricsRowsWithStats forwards to the leaf-owned ranker so the
// staying data reader keeps its call site unchanged. Delete with
// code_call_graph_metrics.go's handler move.
func callGraphMetricsRowsWithStats(
	req callGraphMetricsRequest,
	edgeRows []map[string]any,
) ([]map[string]any, callGraphMetricsStats) {
	return codemodel.CallGraphMetricsRowsWithStats(req, edgeRows)
}

// deadCodeDowngradedRoots aliases the leaf-owned verdict map so the staying
// verdict loader, dead-code readers, and tests keep their signatures and
// literals unchanged. The type split to codemodel with its IsDowngraded
// predicate in L1 (Go requires methods to live with their declaration).
// Delete with code_dead_code_verdicts.go's loader move.
type deadCodeDowngradedRoots = codemodel.DeadCodeDowngradedRoots

// rubyRailsControllerActionRootKind aliases the leaf-owned downgradable
// root kind so the staying verdict tests keep building fixtures with it.
// Delete with code_dead_code_verdicts.go's loader move.
const rubyRailsControllerActionRootKind = codemodel.RubyRailsControllerActionRootKind

// CodeFlowKind aliases the leaf-owned read-surface selector so the staying
// code-flow handlers and payload builders keep their signatures unchanged.
// Delete with code_flow.go's handler move.
type CodeFlowKind = codemodel.CodeFlowKind

// CodeFlowFilter aliases the leaf-owned store filter so the staying
// handlers keep constructing it unchanged. Delete with code_flow.go's
// handler move.
type CodeFlowFilter = codemodel.CodeFlowFilter

// CodeFlowReadModel aliases the leaf-owned store snapshot so the staying
// handlers and payload builders keep their signatures unchanged. Delete
// with code_flow.go's handler move.
type CodeFlowReadModel = codemodel.CodeFlowReadModel

// CodeFlowFunction aliases the leaf-owned parser dataflow row so the
// staying payload builders keep their signatures unchanged. Delete with
// code_flow.go's handler move.
type CodeFlowFunction = codemodel.CodeFlowFunction

// CodeFlowTaintPath aliases the leaf-owned taint evidence row so the
// staying payload builders keep their signatures unchanged. Delete with
// code_flow.go's handler move.
type CodeFlowTaintPath = codemodel.CodeFlowTaintPath

// codeFlowDefaultLimit aliases the leaf-owned page bound so the staying
// request normalization keeps clamping through it. Delete with
// code_flow.go's handler move.
const codeFlowDefaultLimit = codemodel.CodeFlowDefaultLimit

// codeFlowMaxLimit aliases the leaf-owned page cap so the staying request
// normalization keeps clamping through it. Delete with code_flow.go's
// handler move.
const codeFlowMaxLimit = codemodel.CodeFlowMaxLimit

// CodeFlowKindTaintPath aliases the leaf-owned kind value so the staying
// handlers and payload builders keep naming it. Delete with code_flow.go's
// handler move.
const CodeFlowKindTaintPath = codemodel.CodeFlowKindTaintPath

// CodeFlowKindReachingDef aliases the leaf-owned kind value so the staying
// handlers and payload builders keep naming it. Delete with code_flow.go's
// handler move.
const CodeFlowKindReachingDef = codemodel.CodeFlowKindReachingDef

// CodeFlowKindCFGSummary aliases the leaf-owned kind value so the staying
// handlers and payload builders keep naming it. Delete with code_flow.go's
// handler move.
const CodeFlowKindCFGSummary = codemodel.CodeFlowKindCFGSummary

// CodeFlowKindPDGSummary aliases the leaf-owned kind value so the staying
// handlers and payload builders keep naming it. Delete with code_flow.go's
// handler move.
const CodeFlowKindPDGSummary = codemodel.CodeFlowKindPDGSummary
