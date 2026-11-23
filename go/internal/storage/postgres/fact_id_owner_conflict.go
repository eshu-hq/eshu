// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// factOwnerConflictSampleCap bounds how many conflicting facts one refusal
// names in its log line and error text. The counter carries the full count of
// the conflicting batch.
const factOwnerConflictSampleCap = 10

// factIDOwnerConflict is one incoming fact whose fact_id is already held by
// another scope or generation (issue #11203). It carries identifiers only:
// never a payload.
type factIDOwnerConflict struct {
	FactID               string
	FactKind             string
	IncomingScopeID      string
	IncomingGenerationID string
	StoredScopeID        string
	StoredGenerationID   string
}

// reason names which part of the owner differs, for the counter label: another
// scope wins over another generation of the same scope.
func (c factIDOwnerConflict) reason() string {
	if c.IncomingScopeID != c.StoredScopeID {
		return telemetry.FactIDOwnerConflictReasonScope
	}
	return telemetry.FactIDOwnerConflictReasonGeneration
}

// FactIDOwnerConflictError reports that a save found facts whose fact_id is
// already held by another scope or generation. The streaming save returns it
// at the first conflicting batch, before it hands that batch to afterBatch, so
// nothing is derived from a foreign write. A caller with a refusal channel
// (IngestionStore.commitScopeGeneration) rolls back and refuses the generation;
// any other caller rolls back its own transaction and propagates it. It is not
// retryable: a collision is deterministic.
type FactIDOwnerConflictError struct {
	// ScopeID and GenerationID identify the save that found the conflicts.
	ScopeID      string
	GenerationID string
	// Count is the number of conflicting facts in the conflicting batch.
	Count int
	// ByReason splits Count by the closed telemetry.FactIDOwnerConflictReason*
	// set.
	ByReason map[string]int
	// Samples holds at most factOwnerConflictSampleCap conflicts.
	Samples []factIDOwnerConflict
}

// newFactIDOwnerConflictError folds one batch's conflicts into the error.
func newFactIDOwnerConflictError(scopeID, generationID string, conflicts []factIDOwnerConflict) *FactIDOwnerConflictError {
	err := &FactIDOwnerConflictError{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Count:        len(conflicts),
		ByReason:     make(map[string]int, 2),
	}
	for _, conflict := range conflicts {
		err.ByReason[conflict.reason()]++
		if len(err.Samples) < factOwnerConflictSampleCap {
			err.Samples = append(err.Samples, conflict)
		}
	}
	return err
}

// Error names the save and the first conflict; the full sample rides on the
// ERROR log. No payload is included.
func (e *FactIDOwnerConflictError) Error() string {
	first := ""
	if len(e.Samples) > 0 {
		s := e.Samples[0]
		first = fmt.Sprintf("; first fact %q held by scope %q generation %q", s.FactID, s.StoredScopeID, s.StoredGenerationID)
	}
	return fmt.Sprintf(
		"fact_id_owner_conflict: %d fact(s) in scope %q generation %q have a fact_id held by another scope or generation%s",
		e.Count, e.ScopeID, e.GenerationID, first,
	)
}

// factIDOwnerConflictLogSample is one sample in the refusal log's conflicts
// array. Identifiers only, never a payload. It has exactly the fields of
// factIDOwnerConflict, in order, so a conversion between them compiles only
// while both stay in lockstep.
type factIDOwnerConflictLogSample struct {
	FactID               string `json:"fact_id"`
	FactKind             string `json:"fact_kind"`
	IncomingScopeID      string `json:"incoming_scope_id"`
	IncomingGenerationID string `json:"incoming_generation_id"`
	StoredScopeID        string `json:"stored_scope_id"`
	StoredGenerationID   string `json:"stored_generation_id"`
}

// recordFactIDOwnerConflict makes a refused generation visible to an operator:
// one counter increment per conflicting fact found, labeled by the closed
// reason set (scope, generation), and one ERROR log per refusal carrying the
// scope and generation ids, the count, and a conflicts array of up to
// factOwnerConflictSampleCap samples of fact id, fact kind, and the incoming
// and stored owners. No payload is logged and there is no per-fact log line.
func recordFactIDOwnerConflict(
	ctx context.Context,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	sourceSystem string,
	conflict *FactIDOwnerConflictError,
) {
	if conflict == nil {
		return
	}
	if instruments != nil && instruments.FactIDScopeConflicts != nil {
		for reason, count := range conflict.ByReason {
			instruments.FactIDScopeConflicts.Add(
				context.WithoutCancel(ctx), int64(count),
				metric.WithAttributes(
					telemetry.AttrReason(reason),
					telemetry.AttrWriter(telemetry.FactIDOwnerConflictWriterIngestion),
				),
			)
		}
	}
	if logger == nil {
		return
	}
	attrs := make([]any, 0, 8+len(conflict.Samples))
	for _, attr := range telemetry.ScopeAttrs(conflict.ScopeID, conflict.GenerationID, sourceSystem) {
		attrs = append(attrs, attr)
	}
	attrs = append(attrs,
		slog.Int("conflict_count", conflict.Count),
		slog.Int("sampled", len(conflict.Samples)),
		slog.String(telemetry.LogKeyFailureClass, "fact_id_owner_conflict"),
	)
	// One slice attribute, never one group per sample: the production handler is
	// a JSON handler, and repeated keys keep only the last value in any parser.
	samples := make([]factIDOwnerConflictLogSample, 0, len(conflict.Samples))
	for _, sample := range conflict.Samples {
		samples = append(samples, factIDOwnerConflictLogSample(sample))
	}
	attrs = append(attrs, slog.Any("conflicts", samples))
	logger.ErrorContext(ctx, "fact_id_owner_conflict", attrs...)
}
