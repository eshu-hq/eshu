// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

func (h *EntityHandler) getEntityContextFromContent(ctx context.Context, entityID string) (map[string]any, error) {
	if h == nil || h.Content == nil || entityID == "" {
		return nil, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, nil
	}

	entity, err := h.Content.GetEntityContent(ctx, entityID)
	if err != nil || entity == nil {
		return nil, err
	}
	if !access.allowsRepositoryID(entity.RepoID) {
		return nil, nil
	}

	response := contentEntityToMap(*entity)
	relationshipSet, err := buildContentRelationshipSet(ctx, h.Content, *entity)
	if err != nil {
		return nil, err
	}
	relationships := append([]map[string]any{}, relationshipSet.incoming...)
	relationships = append(relationships, relationshipSet.outgoing...)
	response["relationships"] = relationships
	attachSemanticSummary(response)
	return response, nil
}
