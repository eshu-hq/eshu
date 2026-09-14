// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// ScopeRepositoryReader lists the git repository IDs whose facts belong to a
// scope generation. Nil disables the ownership partition for legacy callers.
type ScopeRepositoryReader interface {
	ListScopeRepositoryIDs(ctx context.Context, scopeID, generationID string) ([]string, error)
}

// resolveOwnership loads the repository IDs this scope may publish. An empty
// set is unclassifiable and preserves the legacy emit-all behavior.
func (h *CrossRepoRelationshipHandler) resolveOwnership(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[string]struct{}, bool, error) {
	if h.ScopeRepos == nil {
		return nil, false, nil
	}
	repoIDs, err := h.ScopeRepos.ListScopeRepositoryIDs(ctx, scopeID, generationID)
	if err != nil {
		return nil, false, err
	}
	own := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if normalized := normalizeReducerRepositoryID(repoID); normalized != "" {
			own[normalized] = struct{}{}
		}
	}
	if len(own) == 0 {
		return nil, false, nil
	}
	return own, true, nil
}

// filterEvidenceFactsBySourceRepos restricts retraction input to repositories
// owned by this scope without mutating the caller's backing array.
func filterEvidenceFactsBySourceRepos(
	facts []relationships.EvidenceFact,
	ownRepos map[string]struct{},
	enforce bool,
) []relationships.EvidenceFact {
	if !enforce {
		return facts
	}
	kept := make([]relationships.EvidenceFact, 0, len(facts))
	for _, fact := range facts {
		source := normalizeReducerRepositoryID(fact.SourceRepoID)
		if source == "" {
			kept = append(kept, fact)
			continue
		}
		if _, ok := ownRepos[source]; ok {
			kept = append(kept, fact)
		}
	}
	return kept
}

// partitionResolvedOwnership enforces one writer per source-repository edge.
// Each resolving scope sees its own and backward evidence; without this split,
// two generations race to stamp the same canonical edge.
func partitionResolvedOwnership(
	resolved []relationships.ResolvedRelationship,
	ownRepos map[string]struct{},
	enforce bool,
) (owned, dropped []relationships.ResolvedRelationship) {
	if !enforce {
		return resolved, nil
	}
	for _, relationship := range resolved {
		source := normalizeReducerRepositoryID(relationship.SourceRepoID)
		if source == "" {
			owned = append(owned, relationship)
			continue
		}
		if _, ok := ownRepos[source]; ok {
			owned = append(owned, relationship)
			continue
		}
		dropped = append(dropped, relationship)
	}
	return owned, dropped
}
