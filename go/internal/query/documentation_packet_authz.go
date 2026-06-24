// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

type documentationEvidencePacketFilter struct {
	FindingID            string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

type documentationEvidencePacketFreshnessFilter struct {
	PacketID             string
	SavedPacketVersion   string
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

func documentationEvidencePacketFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketFilter, bool) {
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

func documentationEvidencePacketFreshnessFilterWithRepositoryAccess(
	ctx context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessFilter, bool) {
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
