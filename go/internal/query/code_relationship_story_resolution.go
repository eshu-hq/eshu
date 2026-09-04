// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	if relationshipStoryGrantBlocked(ctx, req) {
		return relationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
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
				access := codeGrantAccessFilter(ctx)
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
	if req.normalizedQueryType() == "class_hierarchy" {
		candidates = relationshipStoryClassHierarchyCandidates(candidates)
	}
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
	allowed, blocked := codeContentGrantScope(ctx, req.RepoID)
	if blocked {
		return nil, nil
	}
	return relationshipStoryGrantedCandidates(ctx, h.Content, req, allowed)
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
		"handle":      "entity:" + entity.EntityID,
		"name":        entity.EntityName,
		"entity_type": entity.EntityType,
		"file_path":   entity.RelativePath,
		"repo_id":     entity.RepoID,
		"language":    entity.Language,
		"start_line":  entity.StartLine,
		"end_line":    entity.EndLine,
	}
}

// relationshipStoryGrantedCandidates resolves the target-name lookup for
// POST /api/v0/code/relationships/story with the caller's repository grant
// bound at the read.
//
// The lookup used to end at SearchEntitiesByNameAnyRepo whenever the request
// named no repo_id, and its rows become the `ambiguous` response's candidate
// list -- entity ids, names, file paths and repository ids, one per match. A
// scoped caller who named a symbol that exists in more than one tenant read the
// other tenant's copy straight out of that list, without ever resolving a
// story.
//
// The three branches match what the route already did, each with the grant
// added:
//
//   - language named: the shared grant-bound entity search, which pushes the
//     granted repository ids into the statement's own WHERE when the store can
//     take them and otherwise asks one granted repository at a time.
//   - repo_id named: unchanged. applyRepositorySelectorForCapability already
//     resolved it against the grant, so an ungranted one never reaches here.
//   - neither: a scoped caller reads its granted repositories one at a time
//     rather than the whole corpus, the same fallback shape
//     symbolNameFallbackEntities (code_symbol.go) uses. An unscoped caller
//     keeps the corpus-wide read.
func relationshipStoryGrantedCandidates(
	ctx context.Context,
	content ContentStore,
	req relationshipStoryRequest,
	allowed []string,
) ([]EntityContent, error) {
	limit := req.normalizedLimit() + 1
	target := req.target()
	repoID := strings.TrimSpace(req.RepoID)
	if language := strings.TrimSpace(req.Language); language != "" {
		return searchEntitiesForGrant(ctx, content, languageEntitySearch{
			RepoID:               repoID,
			Language:             language,
			Query:                target,
			Limit:                limit,
			AllowedRepositoryIDs: allowed,
		})
	}
	if repoID != "" {
		return content.SearchEntitiesByName(ctx, repoID, "", target, limit)
	}
	if len(allowed) == 0 {
		return content.SearchEntitiesByNameAnyRepo(ctx, "", target, limit)
	}
	candidates := make([]EntityContent, 0, limit)
	for _, allowedRepoID := range allowed {
		if len(candidates) >= limit {
			break
		}
		rows, err := content.SearchEntitiesByName(ctx, allowedRepoID, "", target, limit-len(candidates))
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, rows...)
	}
	return candidates, nil
}

// relationshipStoryGrantBlocked reports whether the caller's grant admits
// nothing, so the route must answer its own not-found story without reading a
// backend.
//
// not_found rather than an error or an empty-but-distinguishable shape: it is
// the same answer a target that does not exist produces, so a grantless caller
// cannot use this route to probe which symbols the index holds.
func relationshipStoryGrantBlocked(ctx context.Context, req relationshipStoryRequest) bool {
	_, blocked := codeContentGrantScope(ctx, req.RepoID)
	return blocked
}
