// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import "log/slog"

// Named limitations / partial_reasons / stage-log failure_class values for the
// auxiliary graph reads of the repository context, story, and workload/service
// context responses (#6810). A read that fails (graph deadline, unavailable
// backend) still answers 200 because each panel is auxiliary, but the failure
// must stay visible through a stable reason instead of rendering as an
// authoritative empty list. They follow InfrastructureReadDegradedReason
// (#5764), one per read so an operator can tell which panel degraded. Only
// the two reasons another package names are exported, matching the
// infrastructure pair.
const (
	// RelationshipsReadDegradedReason marks a failed outgoing dependencies
	// read; the entity package names it for workload and service context.
	RelationshipsReadDegradedReason = "relationships_read_degraded"
	// APISurfaceReadDegradedReason marks a failed API surface read; the
	// service package names it for service enrichment.
	APISurfaceReadDegradedReason = "api_surface_read_degraded"
)

const (
	consumersReadDegradedReason                   = "consumers_read_degraded"
	relationshipOverviewReadDegradedReason        = "relationship_overview_read_degraded"
	relationshipsReadDegradedReason               = RelationshipsReadDegradedReason
	languagesReadDegradedReason                   = "languages_read_degraded"
	sourceToolBreakdownReadDegradedReason         = "source_tool_breakdown_read_degraded"
	entryPointsReadDegradedReason                 = "entry_points_read_degraded"
	apiSurfaceReadDegradedReason                  = APISurfaceReadDegradedReason
	deployableUnitRelationshipsReadDegradedReason = "deployable_unit_relationships_read_degraded"
)

// degradedReadLogAttrs builds the stage-log attributes a query stage timer
// attaches after an auxiliary read: the row count, and the read's named
// reason as failure_class when the read failed, so the stage log carries the
// same attribution the response's partial_reasons does.
func degradedReadLogAttrs(rowCount int, degraded bool, reason string) []slog.Attr {
	attrs := []slog.Attr{slog.Int("row_count", rowCount)}
	if degraded {
		attrs = append(attrs, slog.String("failure_class", reason))
	}
	return attrs
}
