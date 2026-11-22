// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Fact id owner-conflict reasons (#11203), recorded on the existing reason
// dimension of eshu_dp_fact_id_scope_conflicts_total. The set is closed.
const (
	// FactIDOwnerConflictReasonScope means the fact_id is already held by a
	// different scope.
	FactIDOwnerConflictReasonScope = "scope"
	// FactIDOwnerConflictReasonGeneration means the fact_id is already held by
	// a different generation of the same scope.
	FactIDOwnerConflictReasonGeneration = "generation"
)

// Fact id owner-conflict writers (#11213), recorded on the existing writer
// dimension of eshu_dp_fact_id_scope_conflicts_total. The set is closed: the
// collector save path and the three reducer fact writers.
const (
	// FactIDOwnerConflictWriterIngestion is the collector ingestion save.
	FactIDOwnerConflictWriterIngestion = "ingestion"
	// FactIDOwnerConflictWriterReducerSingle is the reducer single-row insert.
	FactIDOwnerConflictWriterReducerSingle = "reducer_single"
	// FactIDOwnerConflictWriterReducerBatch is the reducer batched insert.
	FactIDOwnerConflictWriterReducerBatch = "reducer_batch"
	// FactIDOwnerConflictWriterReducerVersioned is the reducer versioned batched insert.
	FactIDOwnerConflictWriterReducerVersioned = "reducer_versioned"
)

// registerFactIDOwnerConflictInstruments registers the #11203 fact id scope
// conflict counter on inst. One increment per conflicting fact found, labeled only by the
// closed reason set: no scope, generation, or fact id label, which are
// unbounded. The scope and generation ids ride on the fact_id_owner_conflict
// ERROR log.
func registerFactIDOwnerConflictInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.FactIDScopeConflicts, err = meter.Int64Counter(
		"eshu_dp_fact_id_scope_conflicts_total",
		metric.WithDescription("Facts whose fact_id is already held by another scope or generation, by reason (scope: the stored scope differs; generation: the scope matches and the generation differs) and writer (ingestion: the streaming fact save; reducer_single, reducer_batch, reducer_versioned: the reducer fact statements) (#11203, #11213). Counted per conflicting fact found. The ingestion save stops at the first conflicting batch and refuses the generation; a reducer write is refused and its intent dead-letters (fact_id_owner_conflict). Either way a non-zero rate is a producer minting an id that omits scope or generation"),
		metric.WithUnit("{fact}"),
	); err != nil {
		return fmt.Errorf("register FactIDScopeConflicts counter: %w", err)
	}
	return nil
}
