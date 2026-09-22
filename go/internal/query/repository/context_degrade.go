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
// (#5764), one per read so an operator can tell which panel degraded.
// Exported so root stayers and the entity package can name them.
const (
	// ConsumersReadDegradedReason marks a failed incoming-consumers read.
	ConsumersReadDegradedReason = "consumers_read_degraded"
	// RelationshipOverviewReadDegradedReason marks a failed relationship
	// overview read in either direction.
	RelationshipOverviewReadDegradedReason = "relationship_overview_read_degraded"
	// RelationshipsReadDegradedReason marks a failed outgoing dependencies read.
	RelationshipsReadDegradedReason = "relationships_read_degraded"
	// LanguagesReadDegradedReason marks a failed language distribution read.
	LanguagesReadDegradedReason = "languages_read_degraded"
	// SourceToolBreakdownReadDegradedReason marks a failed source-tool
	// breakdown read.
	SourceToolBreakdownReadDegradedReason = "source_tool_breakdown_read_degraded"
	// EntryPointsReadDegradedReason marks a failed entry-point read.
	EntryPointsReadDegradedReason = "entry_points_read_degraded"
	// APISurfaceReadDegradedReason marks a failed API surface read.
	APISurfaceReadDegradedReason = "api_surface_read_degraded"
	// DeployableUnitRelationshipsReadDegradedReason marks a failed
	// deployable-unit relationship read.
	DeployableUnitRelationshipsReadDegradedReason = "deployable_unit_relationships_read_degraded"
)

// In-package spellings, matching the infrastructure pair above.
const (
	consumersReadDegradedReason                   = ConsumersReadDegradedReason
	relationshipOverviewReadDegradedReason        = RelationshipOverviewReadDegradedReason
	relationshipsReadDegradedReason               = RelationshipsReadDegradedReason
	languagesReadDegradedReason                   = LanguagesReadDegradedReason
	sourceToolBreakdownReadDegradedReason         = SourceToolBreakdownReadDegradedReason
	entryPointsReadDegradedReason                 = EntryPointsReadDegradedReason
	apiSurfaceReadDegradedReason                  = APISurfaceReadDegradedReason
	deployableUnitRelationshipsReadDegradedReason = DeployableUnitRelationshipsReadDegradedReason
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
