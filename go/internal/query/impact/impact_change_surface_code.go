// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
)

// impact_change_surface_code.go holds the content-store (Postgres) side of
// change-surface resolution -- code-topic evidence and changed-path symbol
// lookup -- split out of impact_change_surface_response.go (which owns the
// graph-traversal impact rows and final response shaping) to keep both files
// under the repo's file-length cap.

func (h *ImpactHandler) changeSurfacePathSymbols(
	ctx context.Context,
	req ChangeSurfaceInvestigationRequest,
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
