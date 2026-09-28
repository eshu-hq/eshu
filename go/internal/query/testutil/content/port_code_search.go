// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/taxonomy"
)

// SearchCodeCandidates mirrors the production read's filter-before-limit
// behavior over the fake's entity fixture.
func (f FakePortContentStore) SearchCodeCandidates(
	_ context.Context,
	repoID, pattern, language string,
	limit int,
	exact bool,
) (nameMatches, sourceMatches []querycontract.EntityContent, err error) {
	allowedLanguages := make(map[string]bool)
	if strings.TrimSpace(language) != "" {
		for _, variant := range taxonomy.NormalizedLanguageVariants(language) {
			allowedLanguages[variant] = true
		}
	}
	rows := slices.Clone(f.Entities)
	slices.SortFunc(rows, func(a, b querycontract.EntityContent) int {
		if n := strings.Compare(a.RelativePath, b.RelativePath); n != 0 {
			return n
		}
		if a.StartLine != b.StartLine {
			return a.StartLine - b.StartLine
		}
		return strings.Compare(a.EntityID, b.EntityID)
	})
	for _, row := range rows {
		if row.RepoID != repoID || (len(allowedLanguages) > 0 && !allowedLanguages[row.Language]) {
			continue
		}
		nameMatchesPattern := row.EntityName == pattern
		if !exact {
			nameMatchesPattern = strings.Contains(strings.ToLower(row.EntityName), strings.ToLower(pattern))
		}
		if nameMatchesPattern && len(nameMatches) < limit {
			nameMatches = append(nameMatches, row)
		}
		if !exact && strings.Contains(strings.ToLower(row.SourceCache), strings.ToLower(pattern)) && len(sourceMatches) < limit {
			sourceMatches = append(sourceMatches, row)
		}
	}
	return nameMatches, sourceMatches, nil
}
