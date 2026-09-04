// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
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
// intent carries no entity keys. It delegates to the package-level
// [AcceptanceUnitID] (issue #6061 review) so this method and [KeyFromScope]
// can never derive a different acceptance unit for the same scope/entity-key
// pair.
func (a IntentAnchor) AcceptanceUnitID() string {
	return AcceptanceUnitID(a.ScopeID, a.EntityKeys)
}

// StateForIntent builds the readiness publication for one intent anchor. The
// second result is false when the anchor cannot name a bounded slice (blank
// scope or generation), in which case the caller must skip publication
// rather than write a partially-keyed row.
//
// It delegates key construction to [KeyFromScope] (issue #6061 review)
// rather than trimming fields and building a [PhaseKey] a second time: before
// this, StateForIntent and KeyFromScope independently reimplemented the same
// derivation, so a family reading readiness through KeyFromScope (iamcan,
// obscoverage) and a publisher writing it through StateForIntent (every
// reducer-root publisher, since this hoist routes them all through
// [StateForIntentValue]) could silently drift onto two different keys for
// the same intent if either derivation were ever edited without the other.
// They happened to already agree — verified before this change, not assumed
// — but agreement by construction is not a guarantee; delegating makes it
// one.
func StateForIntent(
	anchor IntentAnchor,
	keyspace Keyspace,
	phase Phase,
	observedAt time.Time,
) (PhaseState, bool) {
	key, ok := KeyFromScope(anchor.ScopeID, anchor.GenerationID, anchor.EntityKeys, keyspace)
	if !ok {
		return PhaseState{}, false
	}

	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	return PhaseState{
		Key:         key,
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
