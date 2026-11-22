// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Fact id owner-conflict reasons (#11203), recorded on the existing reason
// dimension of eshu_dp_fact_id_owner_conflicts_total. The set is closed.
const (
	// FactIDOwnerConflictReasonScope means the fact_id is already held by a
	// different scope.
	FactIDOwnerConflictReasonScope = "scope"
	// FactIDOwnerConflictReasonGeneration means the fact_id is already held by
	// a different generation of the same scope.
	FactIDOwnerConflictReasonGeneration = "generation"
)

// registerFactIDOwnerConflictInstruments registers the #11203 fact id owner
// conflict counter on inst. One increment per skipped fact, labeled only by the
// closed reason set: no scope, generation, or fact id label, which are
// unbounded. The scope and generation ids ride on the fact_id_owner_conflict
// WARN log.
func registerFactIDOwnerConflictInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.FactIDOwnerConflicts, err = meter.Int64Counter(
		"eshu_dp_fact_id_owner_conflicts_total",
		metric.WithDescription("Facts the streaming fact save skipped because their fact_id is already held by another scope or generation, by reason (scope, generation) (#11203). Any non-zero rate is a producer minting an id that omits scope or generation; the skipped fact was not written"),
		metric.WithUnit("{fact}"),
	); err != nil {
		return fmt.Errorf("register FactIDOwnerConflicts counter: %w", err)
	}
	return nil
}
