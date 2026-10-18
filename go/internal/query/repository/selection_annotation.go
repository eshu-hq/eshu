// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

// PageSelectionReader annotates one repository list page with selection
// aggregates (#8454). Production wiring passes the Postgres batch store;
// nil disables annotation and every entry reads unknown.
type PageSelectionReader interface {
	// ReadPageSelectionsBySlug aggregates the page's repo slugs in batched
	// reads: one slug-resolution statement plus one live-rows statement,
	// with the generation and successor statements only when the page
	// needs them.
	ReadPageSelectionsBySlug(ctx context.Context, slugs []string) (selection.PageSelections, error)
}

// annotateListRepositoriesWithSelection adds the "selection" aggregate to
// each repo map from one batched read for the page (#8454): the same
// selection.Summarize the freshness read uses, rendered by the same block,
// so a list entry and the freshness verdict for one scope can never
// disagree. Slugs come from the repo_slug both list paths already carry; a
// row whose slug resolves to no scope reads the unknown aggregate. An
// annotation read error fails the list: entries never serve with evidence
// silently missing.
func annotateListRepositoriesWithSelection(ctx context.Context, store PageSelectionReader, repos []map[string]any) ([]map[string]any, error) {
	if store == nil {
		for _, repo := range repos {
			repo["selection"] = repositoryFreshnessSelectionToMap(selection.Summary{})
		}
		return repos, nil
	}
	slugs := make([]string, 0, len(repos))
	for _, repo := range repos {
		if slug, ok := repo["repo_slug"].(string); ok && strings.TrimSpace(slug) != "" {
			slugs = append(slugs, slug)
		}
	}
	page, err := store.ReadPageSelectionsBySlug(ctx, slugs)
	if err != nil {
		return nil, fmt.Errorf("annotate repository list with selection: %w", err)
	}
	for _, repo := range repos {
		var summary selection.Summary
		if slug, ok := repo["repo_slug"].(string); ok {
			if scopeID, resolved := page.ScopeBySlug[strings.ToLower(slug)]; resolved {
				summary = page.ByScope[scopeID]
			}
		}
		repo["selection"] = repositoryFreshnessSelectionToMap(summary)
	}
	return repos, nil
}
