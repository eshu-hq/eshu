// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// newEnvelope constructs one observed-confidence submodule fact envelope.
// Every submodule fact shares this construction so schema version, collector
// kind, and source reference stay consistent with the reducer's future
// expectations (mirrors internal/collector/codeowners's newEnvelope).
func newEnvelope(ctx FixtureContext, factKind, stableKey, sourceRecordID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           submoduleFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.SubmoduleSchemaVersionV1,
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

// submoduleFactID derives the content-stable fact identity. Re-emitting an
// unchanged submodule pin in a new generation reuses the same stable key, so
// the fact store upserts rather than duplicates.
func submoduleFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("SubmoduleFact", map[string]any{
		"fact_kind":       factKind,
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
