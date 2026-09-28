// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

// BacklogScopesQueryForTest exposes the candidate statement to the external
// plan-shape test (#7127 ruling 8.10: the candidate read is in the 8.4 sweep).
const BacklogScopesQueryForTest = backlogScopesQuery

// Retention statements for the external plan-shape test (#7127 ruling 8.4's
// sweep covers every statement that reads a ledger table by scope).
const (
	RetentionPruneQueryForTest     = retentionPruneQuery
	RetentionRowCountsQueryForTest = retentionRowCountsQuery
	RetentionDoomedLinksCTEForTest = retentionDoomedLinksCTE
)
