// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
)

// This file owns the boundary between CodeHandler and the deadcode
// subpackage (#6060 lane A). Dead-code analysis lives in deadcode behind
// deadcode.Analyzer; the methods below exist because staying callers --
// the Mount route table and tests that drive the handler -- still name
// them on *CodeHandler. Each delegate builds a per-call Analyzer from the
// handler's own fields, so no wiring changes and no shared mutable state.
// The two queryplan-pinned row readers (deadCodeCandidateRows,
// deadCodeResultsWithGraphIncomingEdges) are NOT here; they stay
// byte-identical in code_dead_code_scan.go and
// code_dead_code_candidate_entity.go with their source_sha256 pins.

// deadAnalyzer binds a fresh deadcode Analyzer to this handler's fields
// for one call.
func (h *CodeHandler) deadAnalyzer() *deadcode.Analyzer {
	return deadcode.NewAnalyzer(deadcode.Dependencies{
		Content: h.Content,
		Graph:   h.Neo4j,
		Profile: h.profile(),

		CandidateRows: h.deadCodeCandidateRows,
		IncomingEdges: h.deadCodeResultsWithGraphIncomingEdges,
		ApplySelector: h.applyRepositorySelectorForCapability,
		GrantFilter:   codeGrantAccessFilter,
		GrantScope:    codeContentGrantScope,

		ResultEntityIDs: deadCodeResultEntityIDs,
		NextCalls:       deadCodeInvestigationNextCalls,
		StartSpan:       startQueryHandlerSpan,
		MergeMetadata:   mergeGraphAndContentMetadata,
		FilterResponse:  filterRelationshipResponse,

		WriteSuccess:        WriteSuccess,
		WriteError:          WriteError,
		WriteContractError:  WriteContractError,
		WriteGraphReadError: WriteGraphReadError,
		ReadJSON:            ReadJSON,
	})
}

// handleDeadCode serves POST /api/v0/code/dead-code.
func (h *CodeHandler) handleDeadCode(w http.ResponseWriter, r *http.Request) {
	h.deadAnalyzer().HandleDeadCode(w, r)
}

// handleCrossRepoDeadCode serves POST /api/v0/code/dead-code/cross-repo.
func (h *CodeHandler) handleCrossRepoDeadCode(w http.ResponseWriter, r *http.Request) {
	h.deadAnalyzer().HandleCrossRepoDeadCode(w, r)
}

// handleDeadCodeInvestigation serves POST /api/v0/code/dead-code/investigate.
func (h *CodeHandler) handleDeadCodeInvestigation(w http.ResponseWriter, r *http.Request) {
	h.deadAnalyzer().HandleDeadCodeInvestigation(w, r)
}

// scanDeadCodeCandidates runs the candidate scan staying tests drive directly.
func (h *CodeHandler) scanDeadCodeCandidates(ctx context.Context, req deadcode.DeadCodeRequest) (deadcode.DeadCodeCandidateScan, error) {
	return h.deadAnalyzer().ScanDeadCodeCandidates(ctx, req)
}

// scanCrossRepoDeadCodeCandidates runs the cross-repo candidate scan
// staying tests drive directly.
func (h *CodeHandler) scanCrossRepoDeadCodeCandidates(ctx context.Context, req deadcode.CrossRepoDeadCodeRequest) (deadcode.CrossRepoDeadCodeScan, error) {
	return h.deadAnalyzer().ScanCrossRepoDeadCodeCandidates(ctx, req)
}

// scanDeadCodeInvestigation runs the investigation scan staying tests
// drive directly.
func (h *CodeHandler) scanDeadCodeInvestigation(ctx context.Context, req deadcode.DeadCodeInvestigationRequest) (deadcode.DeadCodeInvestigationScan, error) {
	return h.deadAnalyzer().ScanDeadCodeInvestigation(ctx, req)
}

// deadCodeIncomingEntityIDs resolves incoming entity IDs for staying tests.
func (h *CodeHandler) deadCodeIncomingEntityIDs(ctx context.Context, results []map[string]any) (map[string]deadcode.DeadCodeIncomingEdge, error) {
	return h.deadAnalyzer().DeadCodeIncomingEntityIDs(ctx, results)
}

// filterDeadCodeResultsWithoutIncomingEdges filters unreachable results
// for staying tests.
func (h *CodeHandler) filterDeadCodeResultsWithoutIncomingEdges(ctx context.Context, results []map[string]any, label string) ([]map[string]any, error) {
	return h.deadAnalyzer().FilterDeadCodeResultsWithoutIncomingEdges(ctx, results, label)
}

// loadDeadCodeDowngradedRoots loads downgraded verdict roots for staying tests.
func (h *CodeHandler) loadDeadCodeDowngradedRoots(ctx context.Context, results []map[string]any) codemodel.DeadCodeDowngradedRoots {
	return h.deadAnalyzer().LoadDeadCodeDowngradedRoots(ctx, results)
}
