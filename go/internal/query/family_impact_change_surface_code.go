// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query // production ChangeSurfaceCodeBackend: takes *ImpactHandler and lane-A code-topic rows, neither nameable from impact/ (#6060).

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Change-surface code backends live in root because they are coupled to
// lane-A codeTopic* types, which cannot cross into the impact subpackage.
// ImpactHandler.CodeSurface carries the production adapter; tests inject
// fakes through the same interface. B2-FOLLOWUP: collapse these back into
// ImpactHandler methods when lane A exports codeTopic*. See #6060.

// changeSurfaceCodeBackend is the production impact.ChangeSurfaceCodeBackend:
// the former (h *ImpactHandler) changeSurfaceCodeSurface, converted to a
// free function with zero body changes (receiver h became an explicit
// parameter; the changed-paths lookup arrives as the caller's bound method
// value so the guard below keeps its exact semantics).
type changeSurfaceCodeBackend struct{}

// NewChangeSurfaceCodeBackend returns the production code-surface backend
// for ImpactHandler wiring.
func NewChangeSurfaceCodeBackend() impact.ChangeSurfaceCodeBackend {
	return changeSurfaceCodeBackend{}
}

// FetchCodeSurface implements impact.ChangeSurfaceCodeBackend.
func (changeSurfaceCodeBackend) FetchCodeSurface(
	ctx context.Context,
	h *ImpactHandler,
	req impact.ChangeSurfaceInvestigationRequest,
	fetchPathSymbols func(context.Context, impact.ChangeSurfaceInvestigationRequest) ([]map[string]any, bool, error),
) (map[string]any, error) {
	// #5167 W3: req.RepoID is used directly below to read topic evidence and
	// changed-path symbols from the content store, bypassing the graph-target
	// resolver's grant filtering entirely -- an explicit repo_id must be
	// checked against the caller's grant before any content read runs.
	if req.RepoID != "" && !impact.ImpactRepoIDAllowed(req.RepoID, querycontract.RepositoryAccessFilterFromContext(ctx)) {
		return nil, impact.ErrChangeSurfaceRepoNotGranted
	}
	files := impact.ChangeSurfaceFileMaps(req.ChangedPaths, req.RepoID)
	symbols := make([]map[string]any, 0)
	evidenceGroups := make([]map[string]any, 0)
	truncated := false
	sourceBackends := []string{}

	if req.Topic != "" {
		rows, err := fetchChangeSurfaceTopicRows(ctx, h, req)
		if err != nil {
			return nil, err
		}
		// #5167 W3: InvestigateCodeTopic (POST /api/v0/code/topics/investigate,
		// the "code/*" family, a different #5167 workstream) has no grant
		// filtering of its own yet -- a topic search with no repo_id scans the
		// whole content-entity corpus. Bind every evidence row to the caller's
		// grant here, independent of that family's own remediation, so this
		// route never surfaces another tenant's code content through a topic
		// search.
		rows = filterCodeTopicRowsForAccess(rows, querycontract.RepositoryAccessFilterFromContext(ctx))
		truncated = len(rows) > req.Limit
		if truncated {
			rows = rows[:req.Limit]
		}
		sourceBackends = append(sourceBackends, "postgres_content_store")
		for index, row := range rows {
			files = codequery.AppendMatchedFile(files, row)
			if row.EntityID != "" {
				symbols = append(symbols, codequery.CodeTopicSymbol(row, index+1))
			}
			evidenceGroups = append(evidenceGroups, codequery.CodeTopicEvidenceGroup(row, index+1))
		}
	}
	pathSymbolsTruncated := false
	if len(req.ChangedPaths) > 0 && h != nil && h.Content != nil {
		pathSymbols, symbolsTruncated, err := fetchPathSymbols(ctx, req)
		if err != nil {
			return nil, err
		}
		symbols = impact.AppendUniqueSymbolMaps(symbols, pathSymbols)
		pathSymbolsTruncated = symbolsTruncated
		sourceBackends = append(sourceBackends, "postgres_content_store")
	}
	truncated = truncated || pathSymbolsTruncated

	return map[string]any{
		"topic":              req.Topic,
		"changed_files":      files,
		"matched_file_count": len(files),
		"touched_symbols":    symbols,
		"symbol_count":       len(symbols),
		"evidence_groups":    evidenceGroups,
		"truncated":          truncated,
		"source_backends":    impact.UniqueStrings(sourceBackends),
		"coverage": map[string]any{
			"query_shape":         "content_topic_and_changed_path_surface",
			"changed_path_count":  len(req.ChangedPaths),
			"changed_path_lookup": "path_scoped",
			"returned_symbols":    len(symbols),
			"limit":               req.Limit,
			"offset":              req.Offset,
			"truncated":           truncated,
		},
	}, nil
}

// fetchChangeSurfaceTopicRows is the former (h *ImpactHandler)
// changeSurfaceTopicRows, converted to a free function with zero body
// changes. It stays in root because it names lane-A codeTopic* types.
func fetchChangeSurfaceTopicRows(ctx context.Context, h *ImpactHandler, req impact.ChangeSurfaceInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error) {
	if h == nil || h.Content == nil {
		return nil, codequery.ErrCodeTopicBackendUnavailable
	}
	investigator, ok := h.Content.(codequery.CodeTopicContentInvestigator)
	if !ok {
		return nil, codequery.ErrCodeTopicBackendUnavailable
	}
	topicReq := codequery.CodeTopicInvestigationRequest{
		Topic:  req.Topic,
		RepoID: req.RepoID,
		Limit:  req.Limit + 1,
		Offset: req.Offset,
		Intent: "change_surface",
		Terms:  codequery.CodeTopicSearchTerms(req.Topic, "change_surface", nil),
	}
	// #5167 W3 P1: when the search is corpus-wide (no explicit repo_id), push the
	// caller's grant into the content-store SQL WHERE so its LIMIT is taken from
	// the granted set, not a cross-tenant-polluted page. filterCodeTopicRowsForAccess
	// below stays as defense-in-depth.
	if req.RepoID == "" {
		if access := querycontract.RepositoryAccessFilterFromContext(ctx); access.Scoped() {
			topicReq.AllowedRepositoryIDs = access.RepositorySearchIDs()
		}
	}
	rows, err := investigator.InvestigateCodeTopic(ctx, topicReq)
	if err != nil {
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	return rows, nil
}

// filterCodeTopicRowsForAccess drops codequery.CodeTopicEvidenceRow entries whose
// RepoID is outside the caller's grant. InvestigateCodeTopic (the "code/*"
// #5167 family) has no grant filtering of its own, so change-surface callers
// that fold topic evidence into their response bind it here independently
// (see changeSurfaceCodeBackend.FetchCodeSurface). It stays in root because
// it names the lane-A row type.
func filterCodeTopicRowsForAccess(rows []codequery.CodeTopicEvidenceRow, access querycontract.RepositoryAccessFilter) []codequery.CodeTopicEvidenceRow {
	if !access.Scoped() {
		return rows
	}
	filtered := make([]codequery.CodeTopicEvidenceRow, 0, len(rows))
	for _, row := range rows {
		if impact.ImpactRepoIDAllowed(row.RepoID, access) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
