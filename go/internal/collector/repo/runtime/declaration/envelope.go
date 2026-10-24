// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// newEnvelope constructs one observed-confidence runtime_declaration fact
// envelope. Every fact shares this construction so schema version, collector
// kind, and source reference stay consistent with the contract.
func newEnvelope(ctx FixtureContext, stableKey, sourceRecordID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           runtimeDeclarationFactID(stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         facts.RuntimeDeclarationVersionFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.RuntimeDeclarationSchemaVersionV1,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceURI:      ctx.SourceURI,
			SourceRecordID: sourceRecordID,
		},
	}
}

// runtimeDeclarationFactID derives the content-stable fact identity. Re-emitting
// an unchanged declaration in a new generation reuses the same stable key, so
// the fact store upserts rather than duplicates.
func runtimeDeclarationFactID(stableFactKey, scopeID, generationID string) string {
	return facts.StableID("RuntimeDeclarationFact", map[string]any{
		"fact_kind":       facts.RuntimeDeclarationVersionFactKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}
