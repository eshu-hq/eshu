// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
)

// The #5363 ContentStore additions on the shared fakePortContentStore double
// live here, split out of ports_test.go to keep that file under the repo's
// 500-line package-file cap. Both read from f.entities so a single fixture set
// drives the name-anchored fetch, the narrow candidate scan, and the by-ID
// hydration consistently.

// ListRepoEntitiesByIDs returns f.entities whose entity_id is in the requested
// set, repo-filtered, ordered deterministically by relative_path/start_line/
// entity_id to mirror the production ContentReader.ListRepoEntitiesByIDs.
func (f fakePortContentStore) ListRepoEntitiesByIDs(_ context.Context, repoID string, entityIDs []string, limit int) ([]EntityContent, error) {
	idSet := make(map[string]struct{}, len(entityIDs))
	for _, id := range entityIDs {
		idSet[id] = struct{}{}
	}
	filtered := make([]EntityContent, 0, len(entityIDs))
	for _, entity := range f.entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if _, ok := idSet[entity.EntityID]; !ok {
			continue
		}
		filtered = append(filtered, entity)
	}
	sortEntityContentByLocation(filtered)
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// ListRepoK8sSelectCandidates projects f.entities' K8sResource rows into the
// narrow K8sSelectCandidate shape via the same helper the production narrow SQL
// mirrors (k8sSelectCandidateFromEntity), preserving the comma-ok tri-state and
// the relative_path/start_line/entity_id ordering.
func (f fakePortContentStore) ListRepoK8sSelectCandidates(_ context.Context, repoID string, limit int) ([]K8sSelectCandidate, error) {
	filtered := make([]EntityContent, 0, len(f.entities))
	for _, entity := range f.entities {
		if repoID != "" && entity.RepoID != "" && entity.RepoID != repoID {
			continue
		}
		if entity.EntityType != "K8sResource" {
			continue
		}
		filtered = append(filtered, entity)
	}
	sortEntityContentByLocation(filtered)
	candidates := make([]K8sSelectCandidate, 0, len(filtered))
	for _, entity := range filtered {
		candidates = append(candidates, k8sSelectCandidateFromEntity(entity))
		if limit > 0 && len(candidates) >= limit {
			break
		}
	}
	return candidates, nil
}

// sortEntityContentByLocation orders rows by relative_path, start_line,
// entity_id, matching the production ORDER BY so the fake's truncation drop set
// and candidate order are deterministic.
func sortEntityContentByLocation(entities []EntityContent) {
	sort.SliceStable(entities, func(i, j int) bool {
		if entities[i].RelativePath != entities[j].RelativePath {
			return entities[i].RelativePath < entities[j].RelativePath
		}
		if entities[i].StartLine != entities[j].StartLine {
			return entities[i].StartLine < entities[j].StartLine
		}
		return entities[i].EntityID < entities[j].EntityID
	})
}
