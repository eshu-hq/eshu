package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// repoReadinessPhaseStates builds one workload-materialization phase-state row
// per distinct repo, keyed by the deterministic per-repo readiness key (#2891)
// so the symbol→runtime shared-projection domains (handles_route, runs_in) can
// find it across the code/workload source-run boundary. It is ADDITIVE: the
// handler still publishes its per-EntityKey workload phase rows for the other
// consumers that depend on them. Blank repo ids are skipped; a zero observedAt
// defers to the wall clock.
func repoReadinessPhaseStates(
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) []GraphProjectionPhaseState {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	states := make([]GraphProjectionPhaseState, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		key := workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)
		if err := key.Validate(); err != nil {
			continue
		}
		states = append(states, GraphProjectionPhaseState{
			Key:         key,
			Phase:       GraphProjectionPhaseWorkloadMaterialization,
			CommittedAt: observedAt,
			UpdatedAt:   observedAt,
		})
	}
	return states
}

// publishRepoReadinessPhases publishes the repo-keyed workload-materialization
// phase rows for the given repos through the existing phase publisher (#2891).
// A nil publisher or empty repo set is a no-op. It builds the rows directly with
// the deterministic per-repo key rather than routing through
// graphProjectionPhaseStateForIntent, which would overwrite the acceptance unit
// with the workload intent's basename-prefixed EntityKey and re-introduce the
// key mismatch this fix removes.
func publishRepoReadinessPhases(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) error {
	if publisher == nil || len(repoIDs) == 0 {
		return nil
	}
	states := repoReadinessPhaseStates(scopeID, generationID, repoIDs, observedAt)
	if len(states) == 0 {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		return fmt.Errorf("publish repo workload-materialization readiness phases: %w", err)
	}
	return nil
}

// projectionRepoReadinessRepoIDs returns the distinct repo ids whose endpoints
// or workloads this projection materialized — the same repos whose endpoints feed
// publishAPIEndpointRepoPathPresence and whose workloads feed
// publishRepoWorkloadPresence. The readiness phase must resolve for exactly these
// repos so handles_route/runs_in rows reach the presence second-gate.
func projectionRepoReadinessRepoIDs(projection *ProjectionResult) []string {
	if projection == nil {
		return nil
	}
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	add := func(repoID string) {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			return
		}
		if _, ok := seen[repoID]; ok {
			return
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	for _, row := range projection.EndpointRows {
		add(row.RepoID)
	}
	for _, row := range projection.WorkloadRows {
		add(row.RepoID)
	}
	return repoIDs
}

// scopeRepositoryGraphIDs returns the distinct repository graph_id values for one
// scope generation. It is the repo-id source for the workload-materialization
// ZERO-candidate path (#2891): a route-only repo materializes no workload or
// endpoint row, so its repo id is not in the projection, yet its handles_route
// rows must still pass the phase gate (and be terminalized by the absent-endpoint
// presence gate) instead of looping forever. The graph_id read here is the SAME
// string the handles_route/runs_in intents carry as repo_id and the workload
// candidates carry as RepoID, so consumer and publisher reconstruct an identical
// readiness key. It uses the bounded repository-kind fact query (one row per
// repo), so it adds no file-fact scan to the hot path.
func scopeRepositoryGraphIDs(
	ctx context.Context,
	loader FactLoader,
	scopeID, generationID string,
) ([]string, error) {
	if loader == nil {
		return nil, nil
	}
	envelopes, err := loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKindRepository})
	if err != nil {
		return nil, fmt.Errorf("load repository facts for repo readiness phases: %w", err)
	}
	return repositoryGraphIDsFromEnvelopes(envelopes), nil
}

// repositoryGraphIDsFromEnvelopes extracts the distinct, sorted repository
// graph_id values from fact envelopes. Sorting makes the published row order
// deterministic for stable telemetry and test assertions.
func repositoryGraphIDsFromEnvelopes(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, env := range envelopes {
		if env.FactKind != factKindRepository {
			continue
		}
		repoID := strings.TrimSpace(payloadStr(env.Payload, "graph_id"))
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	sort.Strings(repoIDs)
	return repoIDs
}
