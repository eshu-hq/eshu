// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/search"
)

// This file owns the boundary between CodeHandler and the deadcode
// subpackage (#6060 lane A). Dead-code analysis lives in deadcode behind
// deadcode.Analyzer; the methods below exist because staying callers --
// the Mount route table and tests that drive the handler -- still name
// them on *CodeHandler. Each delegate builds a per-call Analyzer from the
// handler's own fields, so no wiring changes and no shared mutable state.
// The two queryplan-pinned row readers below (deadCodeCandidateRows,
// deadCodeResultsWithGraphIncomingEdges) are thin *CodeHandler methods
// delegating to the deadcode leaf, with their source_sha256 pins in
// query-source-coverage.yaml.

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

		ResultEntityIDs: deadcode.DeadCodeResultEntityIDs,
		NextCalls:       deadcode.InvestigationNextCalls,
		StartSpan:       startQueryHandlerSpan,
		MergeMetadata:   search.MergeMetadata,
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

func (h *CodeHandler) deadCodeCandidateRows(
	ctx context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	allowedRepositoryIDs, blocked := codeContentGrantScope(ctx, repoID)
	if blocked {
		return nil, nil
	}
	query := deadCodeCandidateQuery{
		RepoID:               repoID,
		Label:                label,
		Language:             language,
		Limit:                limit,
		Offset:               offset,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
	if content, ok := h.Content.(deadCodeCandidateContentStore); ok {
		return content.DeadCodeCandidateRows(ctx, query)
	}
	access := codeGrantAccessFilter(ctx)
	cypher := deadcode.BuildDeadCodeGraphCypherForLabel(repoID != "", label, language, access)
	return h.Neo4j.Run(ctx, cypher, deadcode.DeadCodeGraphParams(repoID, language, limit, offset, access))
}

func (h *CodeHandler) deadCodeResultsWithGraphIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) (map[string]deadCodeIncomingEdge, error) {
	entityIDs := deadcode.DeadCodeResultEntityIDs(results)
	incoming := make(map[string]deadCodeIncomingEdge)
	if len(entityIDs) == 0 {
		return incoming, nil
	}
	access := codeGrantAccessFilter(ctx)
	rows, err := h.Neo4j.Run(
		ctx,
		deadcode.BuildDeadCodeScopedIncomingBatchProbeCypher(label, access),
		access.GraphParams(map[string]any{"entity_ids": entityIDs}),
	)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		entityID := strings.TrimSpace(StringVal(row, "incoming_entity_id"))
		if entityID == "" {
			continue
		}
		// in_grant is projected only by the scoped statement. BoolVal reads an
		// absent column as false, so the unscoped caller -- whose statement has
		// no such column and whose every row is evidence -- must be answered
		// before the column is consulted at all.
		if access.Scoped() && !BoolVal(row, "in_grant") {
			deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{HiddenConsumer: true})
			continue
		}
		method := strings.TrimSpace(StringVal(row, "resolution_method"))
		deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, deadCodeIncomingEdge{
			MaxConfidence: codeprovenance.Confidence(method),
			Method:        method,
		})
	}
	return incoming, nil
}
