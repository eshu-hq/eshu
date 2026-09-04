// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// PhaseState captures one durable readiness publication: the bounded slice the
// publication is about, the milestone it reached, and the commit instant.
type PhaseState struct {
	Key         PhaseKey
	Phase       Phase
	CommittedAt time.Time
	UpdatedAt   time.Time
}

// PhasePublisher persists graph-readiness publications. Implementations must
// be safe under concurrent reducer workers: a republication of the same
// (key, phase) converges on one row rather than failing.
type PhasePublisher interface {
	PublishGraphProjectionPhases(context.Context, []PhaseState) error
}

// IntentAnchor is the bounded intent identity a phase publication needs. It
// carries only the three intent fields the readiness slice is keyed by, so a
// family subpackage can publish a phase without depending on the durable
// Intent value type or on the reducer root that owns the handler wiring.
type IntentAnchor struct {
	ScopeID      string
	GenerationID string
	EntityKeys   []string
}

// AcceptanceUnitID returns the bounded acceptance unit the anchor publishes
// for: the first non-blank entity key, falling back to the scope ID when the
// intent carries no entity keys.
func (a IntentAnchor) AcceptanceUnitID() string {
	for _, entityKey := range a.EntityKeys {
		if trimmed := strings.TrimSpace(entityKey); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(a.ScopeID)
}

// StateForIntent builds the readiness publication for one intent anchor. The
// second result is false when the anchor cannot name a bounded slice (blank
// scope, generation, or acceptance unit), in which case the caller must skip
// publication rather than write a partially-keyed row.
func StateForIntent(
	anchor IntentAnchor,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) (PhaseState, bool) {
	scopeID := strings.TrimSpace(anchor.ScopeID)
	generationID := strings.TrimSpace(anchor.GenerationID)
	if scopeID == "" || generationID == "" {
		return PhaseState{}, false
	}

	acceptanceUnitID := anchor.AcceptanceUnitID()
	if acceptanceUnitID == "" {
		return PhaseState{}, false
	}

	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	return PhaseState{
		Key: PhaseKey{
			ScopeID:          scopeID,
			AcceptanceUnitID: acceptanceUnitID,
			SourceRunID:      generationID,
			GenerationID:     generationID,
			Keyspace:         keyspace,
		},
		Phase:       phase,
		CommittedAt: observedAt,
		UpdatedAt:   observedAt,
	}, true
}

// StateForIntentValue builds the readiness publication for one durable
// reducer.Intent value (issue #6061). It adapts the intent's scope,
// generation, and entity keys to an [IntentAnchor] and delegates to
// [StateForIntent] rather than re-deriving the key, so a caller that already
// holds the durable [reducercontract.Intent] (the reducer root's
// graphProjectionPhaseStateForIntent, before this move) and a family that
// only holds an [IntentAnchor] can never drift onto two different
// derivations of the same key. [IntentAnchor] remains the dependency-free
// option for a family that wants to publish without depending on
// reducercontract; this is a convenience for a caller that already has the
// full intent value.
func StateForIntentValue(
	intent reducercontract.Intent,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) (PhaseState, bool) {
	return StateForIntent(
		IntentAnchor{
			ScopeID:      intent.ScopeID,
			GenerationID: intent.GenerationID,
			EntityKeys:   intent.EntityKeys,
		},
		keyspace,
		phase,
		observedAt,
	)
}
