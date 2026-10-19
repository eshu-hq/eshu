// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
)

func (h *Handler) listRepositoriesFromContent(ctx context.Context) ([]map[string]any, error) {
	if h == nil || h.Content == nil {
		return []map[string]any{}, nil
	}

	entries, err := h.Content.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	repos := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		repos = append(repos, repositoryCatalogMap(entry))
	}
	return repos, nil
}

// getRepositoryContext returns repository metadata with graph statistics and
// enriched context including entry points, infrastructure entities, language
// distribution, cross-repo relationships, and consumer repositories.
// GetRepositoryStory serves the repository story response. It forwards to
// getRepositoryStory; exported for #6060 so the cross-family endpoint
// contract test in package query can name it from outside this package.
