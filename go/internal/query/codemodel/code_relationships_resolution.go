// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const relationshipAmbiguityCandidateLimit = 25

// RelationshipsRequest is the decoded entity-relationship lookup request.
// It split here from root code_relationships.go (#6060 lane A L1) with the
// name-target resolver that reads it. It is exported because the staying
// relationships handler constructs it; root's family_code_shim.go aliases
// it back so the staying handler keeps its literal.
type RelationshipsRequest struct {
	EntityID         string `json:"entity_id"`
	Name             string `json:"name"`
	RepoID           string `json:"repo_id"`
	Direction        string `json:"direction"`
	RelationshipType string `json:"relationship_type"`
	Transitive       bool   `json:"transitive"`
	MaxDepth         int    `json:"max_depth"`
}

// ResolveRelationshipsNameTarget resolves a name-only relationships lookup
// to one exact graph entity, or returns an ambiguity resolution when the
// name matches several.
func ResolveRelationshipsNameTarget(
	ctx context.Context,
	reader querycontract.ContentStore,
	req RelationshipsRequest,
) (*querycontract.EntityContent, *RelationshipStoryResolution, error) {
	candidates, err := resolveExactGraphEntityCandidates(ctx, reader, req.RepoID, req.Name)
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	resolved, err := selectExactGraphEntityCandidate(req.RepoID, req.Name, candidates)
	if err == nil {
		return resolved, nil, nil
	}

	if nonTest := nonTestEntityMatches(candidates); len(nonTest) > 1 {
		candidates = nonTest
	}
	SortRelationshipStoryCandidates(candidates)
	truncated := len(candidates) > relationshipAmbiguityCandidateLimit
	resolution := RelationshipStoryResolution{
		Status:     "ambiguous",
		Target:     strings.TrimSpace(req.Name),
		RepoID:     strings.TrimSpace(req.RepoID),
		Candidates: RelationshipStoryCandidateMaps(candidates, relationshipAmbiguityCandidateLimit),
		Truncated:  truncated,
	}
	return nil, &resolution, nil
}

func AmbiguousRelationshipsResponse(req RelationshipsRequest, resolution RelationshipStoryResolution) map[string]any {
	return map[string]any{
		"status":            "ambiguous",
		"target_resolution": resolution,
		"name":              strings.TrimSpace(req.Name),
		"repo_id":           strings.TrimSpace(req.RepoID),
		"outgoing":          []map[string]any{},
		"incoming":          []map[string]any{},
		"summary": map[string]any{
			"candidate_count": len(resolution.Candidates),
			"truncated":       resolution.Truncated,
		},
	}
}

// The exact-entity resolution chain below is a family-local copy of root's
// entity_resolution.go helpers of the same names. The name-target resolver
// above needs them, and the staying entity, exposure-path, and call-chain
// readers that share them cannot cross the package boundary, so the leaf
// carries these byte-identical copies instead of importing root. Keep them
// behavior-identical to their root sources.
const graphEntityResolutionLimit = 50

func resolveExactGraphEntityCandidates(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	name string,
) ([]querycontract.EntityContent, error) {
	if reader == nil {
		return nil, nil
	}
	repoID = strings.TrimSpace(repoID)
	name = strings.TrimSpace(name)
	if repoID == "" || name == "" {
		return nil, nil
	}

	matches, err := reader.SearchEntitiesByName(ctx, repoID, "", name, graphEntityResolutionLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve graph entity %q in repo %q: %w", name, repoID, err)
	}
	return exactEntityNameMatches(matches, name), nil
}

func selectExactGraphEntityCandidate(repoID string, name string, exact []querycontract.EntityContent) (*querycontract.EntityContent, error) {
	switch len(exact) {
	case 0:
		return nil, nil
	case 1:
		candidate := exact[0]
		return &candidate, nil
	}

	nonTest := nonTestEntityMatches(exact)
	if len(nonTest) == 1 {
		candidate := nonTest[0]
		return &candidate, nil
	}

	return nil, fmt.Errorf(
		"entity name %q in repository %q matched multiple entities: %s",
		name,
		repoID,
		formatAmbiguousEntityMatches(exact),
	)
}

func exactEntityNameMatches(matches []querycontract.EntityContent, name string) []querycontract.EntityContent {
	filtered := make([]querycontract.EntityContent, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.EntityName) != name {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

func nonTestEntityMatches(matches []querycontract.EntityContent) []querycontract.EntityContent {
	filtered := make([]querycontract.EntityContent, 0, len(matches))
	for _, match := range matches {
		if isTestEntityPath(match.RelativePath) {
			continue
		}
		filtered = append(filtered, match)
	}
	return filtered
}

func isTestEntityPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	return strings.HasSuffix(path, "_test.go")
}

func formatAmbiguousEntityMatches(matches []querycontract.EntityContent) string {
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		location := strings.TrimSpace(match.RelativePath)
		if location == "" {
			location = "<unknown>"
		}
		items = append(items, fmt.Sprintf("%s (%s:%d)", match.EntityID, location, match.StartLine))
	}
	slices.Sort(items)
	return strings.Join(items, ", ")
}

// The candidate sort/map shapers below moved here from root
// code_relationship_story_resolution.go (#6060 lane A L1) with the
// name-target resolver that renders through them. Sort and Maps are
// exported because the staying story resolver still calls them via the
// root forwards; SortKey and Map serve only this package.
// SortRelationshipStoryCandidates orders ambiguous name-target candidates
// deterministically for the resolution envelope.
func SortRelationshipStoryCandidates(candidates []querycontract.EntityContent) {
	slices.SortFunc(candidates, func(a, b querycontract.EntityContent) int {
		return strings.Compare(relationshipStoryCandidateSortKey(a), relationshipStoryCandidateSortKey(b))
	})
}

func relationshipStoryCandidateSortKey(entity querycontract.EntityContent) string {
	return strings.Join([]string{
		entity.RepoID,
		entity.RelativePath,
		fmt.Sprintf("%012d", entity.StartLine),
		entity.EntityID,
	}, "\x00")
}

// RelationshipStoryCandidateMaps shapes ambiguous name-target candidates
// into the resolution envelope rows, applying the candidate limit.
func RelationshipStoryCandidateMaps(candidates []querycontract.EntityContent, limit int) []map[string]any {
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	items := make([]map[string]any, 0, len(candidates))
	for _, entity := range candidates {
		items = append(items, relationshipStoryCandidateMap(entity))
	}
	return items
}

func relationshipStoryCandidateMap(entity querycontract.EntityContent) map[string]any {
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
