// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Closed outcome values for eshu_dp_canonical_repository_retirements_total
// (#7324). The canonical writer records one of them only when its
// path-conflict retirement statement deleted a Repository node.
const (
	// RepositoryRetirementOutcomeClean means the retired Repository carried
	// no relationships, so the retirement dropped nothing.
	RepositoryRetirementOutcomeClean = "clean"
	// RepositoryRetirementOutcomeDroppedRelationships means the backend
	// reported at least one relationship deleted with the retired node. The
	// count covers both directions, including the retired node's own
	// projector edges; direction and type are not attributed.
	RepositoryRetirementOutcomeDroppedRelationships = "dropped_relationships"
)

// registerCanonicalRepositoryRetirements registers the path-conflict
// Repository retirement counter (#7324) on inst.
func registerCanonicalRepositoryRetirements(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.CanonicalRepositoryRetirements, err = meter.Int64Counter(
		"eshu_dp_canonical_repository_retirements_total",
		metric.WithDescription("Different-id Repository nodes the canonical writer retired at a re-projected path, by outcome (clean, dropped_relationships) (#7324)"),
	); err != nil {
		return fmt.Errorf("register CanonicalRepositoryRetirements counter: %w", err)
	}
	return nil
}
