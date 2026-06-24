// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

func documentationFindingAggregateFilterWithRepositoryAccess(
	ctx context.Context,
	filter DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateFilter, bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.scoped() {
		return filter, true
	}
	if access.empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	return filter, true
}
