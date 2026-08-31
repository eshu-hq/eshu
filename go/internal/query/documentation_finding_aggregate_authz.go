// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

func documentationFindingAggregateFilterWithRepositoryAccess(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateFilter, bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.Scoped() {
		return filter, true
	}
	if access.Empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter, true
}
