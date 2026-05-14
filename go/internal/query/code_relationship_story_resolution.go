package query

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func (h *CodeHandler) resolveRelationshipStoryTarget(
	ctx context.Context,
	req relationshipStoryRequest,
) (relationshipStoryResolution, *EntityContent, error) {
	target := req.target()
	if entityID := strings.TrimSpace(req.EntityID); entityID != "" {
		resolution := relationshipStoryResolution{
			Status:   "resolved",
			Target:   target,
			EntityID: entityID,
			RepoID:   strings.TrimSpace(req.RepoID),
			Language: strings.TrimSpace(req.Language),
		}
		if h != nil && h.Content != nil {
			entity, err := h.Content.GetEntityContent(ctx, entityID)
			if err != nil {
				return resolution, nil, err
			}
			if entity != nil {
				resolution.Name = entity.EntityName
				resolution.RepoID = entity.RepoID
				resolution.Language = entity.Language
				return resolution, entity, nil
			}
		}
		return resolution, &EntityContent{EntityID: entityID, EntityName: target, RepoID: req.RepoID}, nil
	}
	if h == nil || h.Content == nil {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}

	candidates, err := h.relationshipStoryCandidates(ctx, req)
	if err != nil {
		return relationshipStoryResolution{}, nil, err
	}
	if len(candidates) == 0 {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	candidates = exactEntityNameMatches(candidates, target)
	if len(candidates) == 0 {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	sortRelationshipStoryCandidates(candidates)
	limit := req.normalizedLimit()
	truncated := len(candidates) > limit
	if len(candidates) != 1 {
		return relationshipStoryResolution{
			Status:     "ambiguous",
			Target:     target,
			RepoID:     strings.TrimSpace(req.RepoID),
			Language:   strings.TrimSpace(req.Language),
			Candidates: relationshipStoryCandidateMaps(candidates, limit),
			Truncated:  truncated,
		}, nil, nil
	}
	entity := candidates[0]
	return relationshipStoryResolution{
		Status:   "resolved",
		Target:   target,
		EntityID: entity.EntityID,
		Name:     entity.EntityName,
		RepoID:   entity.RepoID,
		Language: entity.Language,
	}, &entity, nil
}

func (h *CodeHandler) relationshipStoryCandidates(
	ctx context.Context,
	req relationshipStoryRequest,
) ([]EntityContent, error) {
	limit := req.normalizedLimit() + 1
	target := req.target()
	if strings.TrimSpace(req.Language) != "" {
		return h.Content.SearchEntitiesByLanguageAndType(
			ctx,
			strings.TrimSpace(req.RepoID),
			strings.TrimSpace(req.Language),
			"",
			target,
			limit,
		)
	}
	if strings.TrimSpace(req.RepoID) != "" {
		return h.Content.SearchEntitiesByName(ctx, strings.TrimSpace(req.RepoID), "", target, limit)
	}
	return h.Content.SearchEntitiesByNameAnyRepo(ctx, "", target, limit)
}

func sortRelationshipStoryCandidates(candidates []EntityContent) {
	slices.SortFunc(candidates, func(a, b EntityContent) int {
		return strings.Compare(relationshipStoryCandidateSortKey(a), relationshipStoryCandidateSortKey(b))
	})
}

func relationshipStoryCandidateSortKey(entity EntityContent) string {
	return strings.Join([]string{
		entity.RepoID,
		entity.RelativePath,
		fmt.Sprintf("%012d", entity.StartLine),
		entity.EntityID,
	}, "\x00")
}

func relationshipStoryCandidateMaps(candidates []EntityContent, limit int) []map[string]any {
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	items := make([]map[string]any, 0, len(candidates))
	for _, entity := range candidates {
		items = append(items, relationshipStoryCandidateMap(entity))
	}
	return items
}

func relationshipStoryCandidateMap(entity EntityContent) map[string]any {
	return map[string]any{
		"entity_id":   entity.EntityID,
		"name":        entity.EntityName,
		"entity_type": entity.EntityType,
		"file_path":   entity.RelativePath,
		"repo_id":     entity.RepoID,
		"language":    entity.Language,
		"start_line":  entity.StartLine,
		"end_line":    entity.EndLine,
	}
}
