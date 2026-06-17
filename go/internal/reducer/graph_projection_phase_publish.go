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

// publishRepoWorkloadDoneMarkers publishes one workload_materialization phase row
// per repo under the service_uid keyspace keyed by the repo's acceptance unit
// (repository:<repo_id>) with the generation id as the source run (#2892). This
// is the signal the code-stage handles_route/runs_in readiness gate matches: the
// stage-native workload_materialization phase is keyed by the workload/repo
// EntityKey AU and so never matches the bridge intents' repository:<repo_id> AU.
// Without these markers the bridge intents defer forever and the
// HANDLES_ROUTE/RUNS_IN edges never project. repoIDs are the
// repository:<repo_id> graph ids of the repos whose endpoints/workloads committed
// this generation. A nil publisher, blank scope/generation, or empty repo set is
// a no-op.
func publishRepoWorkloadDoneMarkers(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	scope := strings.TrimSpace(scopeID)
	generation := strings.TrimSpace(generationID)
	if scope == "" || generation == "" {
		return nil
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	seen := make(map[string]struct{}, len(repoIDs))
	states := make([]GraphProjectionPhaseState, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		repo := strings.TrimSpace(repoID)
		if repo == "" {
			continue
		}
		if _, exists := seen[repo]; exists {
			continue
		}
		seen[repo] = struct{}{}
		states = append(states, GraphProjectionPhaseState{
			Key: GraphProjectionPhaseKey{
				ScopeID:          scope,
				AcceptanceUnitID: repo,
				SourceRunID:      generation,
				GenerationID:     generation,
				Keyspace:         GraphProjectionKeyspaceServiceUID,
			},
			Phase:       GraphProjectionPhaseWorkloadMaterialization,
			CommittedAt: observedAt,
			UpdatedAt:   observedAt,
		})
	}
	if len(states) == 0 {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		return fmt.Errorf("publish repo workload-done markers: %w", err)
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
