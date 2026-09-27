// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ListRepoEntitiesByKeys returns up to two fixture rows per unique exact key,
// in first-key order then entity ID, matching the production content reader.
func (f FakePortContentStore) ListRepoEntitiesByKeys(
	ctx context.Context,
	repoID string,
	keys []querycontract.EntityContentKey,
) ([]querycontract.EntityContent, error) {
	seen := make(map[querycontract.EntityContentKey]struct{}, len(keys))
	results := make([]querycontract.EntityContent, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if len(seen) > querycontract.MaxEntityContentKeys {
			return nil, fmt.Errorf("list repo entities by keys: exceeds %d unique keys", querycontract.MaxEntityContentKeys)
		}
		var matches []querycontract.EntityContent
		for _, entity := range f.Entities {
			if entity.RepoID != repoID || entity.RelativePath != key.RelativePath ||
				entity.EntityType != key.EntityType || entity.EntityName != key.EntityName ||
				entity.StartLine != key.StartLine {
				continue
			}
			matches = append(matches, entity)
		}
		sort.Slice(matches, func(i, j int) bool { return matches[i].EntityID < matches[j].EntityID })
		if len(matches) > 2 {
			matches = matches[:2]
		}
		results = append(results, matches...)
	}
	return results, nil
}
