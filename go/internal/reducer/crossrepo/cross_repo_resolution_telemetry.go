// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	crossRepoEdgeOutcomeOwnedRouted         = "owned_routed"
	crossRepoEdgeOutcomeForeignOwnedDropped = "foreign_owned_dropped"
)

// recordCrossRepoEdgeOutcomes records bounded ownership outcomes by
// relationship type so a broken single-writer attribution premise is visible
// in metrics instead of only in logs.
func (h *CrossRepoRelationshipHandler) recordCrossRepoEdgeOutcomes(
	ctx context.Context,
	resolved []relationships.ResolvedRelationship,
	outcome string,
) {
	if h.Instruments == nil || len(resolved) == 0 {
		return
	}

	counts := make(map[string]int64)
	for _, relationship := range resolved {
		counts[string(relationship.RelationshipType)]++
	}
	types := make([]string, 0, len(counts))
	for relationshipType := range counts {
		types = append(types, relationshipType)
	}
	sort.Strings(types)
	for _, relationshipType := range types {
		h.Instruments.CrossRepoEdgesResolved.Add(
			ctx,
			counts[relationshipType],
			metric.WithAttributes(
				attribute.String("relationship_type", relationshipType),
				telemetry.AttrOutcome(outcome),
			),
		)
	}
}
