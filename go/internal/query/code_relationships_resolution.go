// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

const relationshipAmbiguityCandidateLimit = 25

func resolveRelationshipsNameTarget(
	ctx context.Context,
	reader ContentStore,
	req relationshipsRequest,
) (*EntityContent, *relationshipStoryResolution, error) {
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
	sortRelationshipStoryCandidates(candidates)
	truncated := len(candidates) > relationshipAmbiguityCandidateLimit
	resolution := relationshipStoryResolution{
		Status:     "ambiguous",
		Target:     strings.TrimSpace(req.Name),
		RepoID:     strings.TrimSpace(req.RepoID),
		Candidates: relationshipStoryCandidateMaps(candidates, relationshipAmbiguityCandidateLimit),
		Truncated:  truncated,
	}
	return nil, &resolution, nil
}

func ambiguousRelationshipsResponse(req relationshipsRequest, resolution relationshipStoryResolution) map[string]any {
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
