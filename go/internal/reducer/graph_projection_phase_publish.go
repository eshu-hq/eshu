package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func publishIntentGraphPhase(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := graphProjectionPhaseStateForIntent(intent, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, []GraphProjectionPhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}

func graphProjectionPhaseStateForIntent(
	intent Intent,
	keyspace GraphProjectionKeyspace,
	phase GraphProjectionPhase,
	observedAt time.Time,
) (GraphProjectionPhaseState, bool) {
	scopeID := strings.TrimSpace(intent.ScopeID)
	generationID := strings.TrimSpace(intent.GenerationID)
	if scopeID == "" || generationID == "" {
		return GraphProjectionPhaseState{}, false
	}

	acceptanceUnitID := graphPhaseAcceptanceUnitID(intent)
	if acceptanceUnitID == "" {
		return GraphProjectionPhaseState{}, false
	}

	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	return GraphProjectionPhaseState{
		Key: GraphProjectionPhaseKey{
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

func graphPhaseAcceptanceUnitID(intent Intent) string {
	for _, entityKey := range intent.EntityKeys {
		if trimmed := strings.TrimSpace(entityKey); trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(intent.ScopeID)
}
