// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) resolveRelationshipStoryTarget(
	ctx context.Context,
	req relationshipStoryRequest,
) (relationshipStoryResolution, *EntityContent, error) {
	target := req.EffectiveTarget()
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
				access := repositoryAccessFilterFromContext(ctx)
				if strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(entity.RepoID) != strings.TrimSpace(req.RepoID) {
					return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
				if !access.AllowsRepositoryID(strings.TrimSpace(entity.RepoID)) {
					return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
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
	if req.NormalizedQueryType() == "class_hierarchy" {
		candidates = relationshipStoryClassHierarchyCandidates(candidates)
	}
	if len(candidates) == 0 {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	sortRelationshipStoryCandidates(candidates)
	limit := req.NormalizedLimit()
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

func relationshipStoryClassHierarchyCandidates(candidates []EntityContent) []EntityContent {
	out := make([]EntityContent, 0, len(candidates))
	for _, candidate := range candidates {
		if relationshipStoryClassHierarchyEntityType(candidate.EntityType) {
			out = append(out, candidate)
		}
	}
	return out
}

func relationshipStoryClassHierarchyEntityType(entityType string) bool {
	switch strings.ToLower(strings.TrimSpace(entityType)) {
	case "class", "interface", "trait", "struct", "enum", "protocol":
		return true
	default:
		return false
	}
}

func (h *CodeHandler) relationshipStoryCandidates(
	ctx context.Context,
	req relationshipStoryRequest,
) ([]EntityContent, error) {
	limit := req.NormalizedLimit() + 1
	target := req.EffectiveTarget()
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

// The candidate sort/map shapers moved to
// codemodel/code_relationships_resolution.go (#6060 lane A L1) with the
// name-target resolver that renders through them; the staying resolver
// calls them through the family_code_shim.go forwards.
