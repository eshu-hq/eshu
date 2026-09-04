// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The packet authorization filters are aliases onto querycontract, so this
// package's call sites keep their unexported spelling while a ContentStore
// double outside package query can still name them (#6060).
type (
	documentationEvidencePacketFilter          = querycontract.DocumentationEvidencePacketFilter
	documentationEvidencePacketFreshnessFilter = querycontract.DocumentationEvidencePacketFreshnessFilter
)

func documentationEvidencePacketFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketFilter, bool) {
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

func documentationEvidencePacketFreshnessFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessFilter, bool) {
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
