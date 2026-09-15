// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const crossRepoEdgeDropReasonForeignOwned = "foreign_owned"

// recordCrossRepoEdgesDropped records bounded ownership drops by relationship
// type so a broken single-writer attribution premise is visible in metrics
// without changing the established resolved-edge counter's meaning.
func (h *CrossRepoRelationshipHandler) recordCrossRepoEdgesDropped(
	ctx context.Context,
	resolved []relationships.ResolvedRelationship,
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
		h.Instruments.CrossRepoEdgesDropped.Add(
			ctx,
			counts[relationshipType],
			metric.WithAttributes(
				attribute.String("relationship_type", relationshipType),
				attribute.String("reason", crossRepoEdgeDropReasonForeignOwned),
			),
		)
	}
}
