// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

// impact_change_surface_code.go holds the content-store (Postgres) side of
// change-surface resolution -- code-topic evidence and changed-path symbol
// lookup -- split out of impact_change_surface_response.go (which owns the
// graph-traversal impact rows and final response shaping) to keep both files
// under the repo's file-length cap.

func (h *ImpactHandler) changeSurfaceCodeSurface(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) (map[string]any, error) {
	// #5167 W3: req.RepoID is used directly below to read topic evidence and
	// changed-path symbols from the content store, bypassing the graph-target
	// resolver's grant filtering entirely -- an explicit repo_id must be
	// checked against the caller's grant before any content read runs.
	if req.RepoID != "" && !impactRepoIDAllowed(req.RepoID, repositoryAccessFilterFromContext(ctx)) {
		return nil, errChangeSurfaceRepoNotGranted
	}
	files := changeSurfaceFileMaps(req.ChangedPaths, req.RepoID)
	symbols := make([]map[string]any, 0)
	evidenceGroups := make([]map[string]any, 0)
	truncated := false
	sourceBackends := []string{}

	if req.Topic != "" {
		rows, err := h.changeSurfaceTopicRows(ctx, req)
		if err != nil {
			return nil, err
		}
		// #5167 W3: investigateCodeTopic (POST /api/v0/code/topics/investigate,
		// the "code/*" family, a different #5167 workstream) has no grant
		// filtering of its own yet -- a topic search with no repo_id scans the
		// whole content-entity corpus. Bind every evidence row to the caller's
		// grant here, independent of that family's own remediation, so this
		// route never surfaces another tenant's code content through a topic
		// search.
		rows = filterCodeTopicRowsForAccess(rows, repositoryAccessFilterFromContext(ctx))
		truncated = len(rows) > req.Limit
		if truncated {
			rows = rows[:req.Limit]
		}
		sourceBackends = append(sourceBackends, "postgres_content_store")
		for index, row := range rows {
			files = appendMatchedFile(files, row)
			if row.EntityID != "" {
				symbols = append(symbols, codeTopicSymbol(row, index+1))
			}
			evidenceGroups = append(evidenceGroups, codeTopicEvidenceGroup(row, index+1))
		}
	}
	pathSymbolsTruncated := false
	if len(req.ChangedPaths) > 0 && h != nil && h.Content != nil {
		pathSymbols, symbolsTruncated, err := h.changeSurfacePathSymbols(ctx, req)
		if err != nil {
			return nil, err
		}
		symbols = appendUniqueSymbolMaps(symbols, pathSymbols)
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
		"source_backends":    uniqueStrings(sourceBackends),
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

func (h *ImpactHandler) changeSurfaceTopicRows(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	if h == nil || h.Content == nil {
		return nil, errCodeTopicBackendUnavailable
	}
	investigator, ok := h.Content.(codeTopicContentInvestigator)
	if !ok {
		return nil, errCodeTopicBackendUnavailable
	}
	topicReq := codeTopicInvestigationRequest{
		Topic:  req.Topic,
		RepoID: req.RepoID,
		Limit:  req.Limit + 1,
		Offset: req.Offset,
		Intent: "change_surface",
		Terms:  codeTopicSearchTerms(req.Topic, "change_surface", nil),
	}
	// #5167 W3 P1: when the search is corpus-wide (no explicit repo_id), push the
	// caller's grant into the content-store SQL WHERE so its LIMIT is taken from
	// the granted set, not a cross-tenant-polluted page. filterCodeTopicRowsForAccess
	// below stays as defense-in-depth.
	if req.RepoID == "" {
		if access := repositoryAccessFilterFromContext(ctx); access.scoped() {
			topicReq.AllowedRepositoryIDs = access.repositorySearchIDs()
		}
	}
	rows, err := investigator.investigateCodeTopic(ctx, topicReq)
	if err != nil {
		return nil, fmt.Errorf("investigate code topic: %w", err)
	}
	return rows, nil
}

func (h *ImpactHandler) changeSurfacePathSymbols(
	ctx context.Context,
	req changeSurfaceInvestigationRequest,
) ([]map[string]any, bool, error) {
	entities, err := h.Content.ListRepoEntitiesByPaths(ctx, req.RepoID, req.ChangedPaths, req.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list repo entities by changed paths: %w", err)
	}
	symbols := make([]map[string]any, 0)
	for _, entity := range entities {
		symbols = append(symbols, map[string]any{
			"entity_id":     entity.EntityID,
			"entity_name":   entity.EntityName,
			"entity_type":   entity.EntityType,
			"repo_id":       entity.RepoID,
			"relative_path": entity.RelativePath,
			"language":      entity.Language,
			"start_line":    entity.StartLine,
			"end_line":      entity.EndLine,
			"source_handle": map[string]any{
				"repo_id":       entity.RepoID,
				"relative_path": entity.RelativePath,
				"start_line":    entity.StartLine,
				"end_line":      entity.EndLine,
			},
		})
	}
	truncated := len(symbols) > req.Limit
	if truncated {
		symbols = symbols[:req.Limit]
	}
	return symbols, truncated, nil
}
