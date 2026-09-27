// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
)

// workloadMaterializationRepoReadinessKey forwards to
// [gpphase.WorkloadMaterializationRepoReadinessKey].
func workloadMaterializationRepoReadinessKey(scopeID, repoID, generationID string) GraphProjectionPhaseKey {
	return gpphase.WorkloadMaterializationRepoReadinessKey(scopeID, repoID, generationID)
}

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

// repoReadinessPresenceKeyspaces are the symbol→runtime presence keyspaces whose
// stale rows must be cleared before a repo's workload-materialization phase gate
// is opened: the (repo_id, path) endpoint presence the handles_route gate reads,
// and the repo→workload presence the runs_in gate reads.
var repoReadinessPresenceKeyspaces = []GraphProjectionKeyspace{
	GraphProjectionKeyspaceAPIEndpointRepoPath,
	GraphProjectionKeyspaceRepoWorkloadPresence,
}

// publishRepoReadinessPhases publishes the repo-keyed workload-materialization
// phase rows for the given repos through the existing phase publisher (#2891).
// A nil publisher or empty repo set is a no-op. It builds the rows directly with
// the deterministic per-repo key rather than routing through
// graphProjectionPhaseStateForIntent, which would overwrite the acceptance unit
// with the workload intent's basename-prefixed EntityKey and re-introduce the
// key mismatch this fix removes.
//
// Before opening the gate it RETRACTS each repo's stale (other-generation)
// presence in both symbol→runtime keyspaces (#2891 review). Publishing the
// readiness phase lets handles_route/runs_in pass the phase gate and reach the
// presence second-gate; if a prior generation's (repo_id, path) endpoint or
// repo→workload presence still lingered — e.g. a generation with no workload
// candidates, or a repo whose endpoints/workloads disappeared, where the per-row
// presence publish is a no-op and never runs its own retract — the presence gate
// would see that stale target as present and project a HANDLES_ROUTE / RUNS_IN
// edge to a node this generation did not materialize, instead of terminalizing
// the absent target. RetractStaleRepoGenerations deletes only OTHER generations'
// rows, so this never removes presence this generation just published. A nil
// presence writer (gate off) skips the retract.
func publishRepoReadinessPhases(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	presenceWriter EndpointPresenceWriter,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) error {
	return publishRepoReadinessPhasesWithRepair(ctx, publisher, nil, presenceWriter, scopeID, generationID, repoIDs, observedAt)
}

func publishRepoReadinessPhasesWithRepair(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	presenceWriter EndpointPresenceWriter,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
) error {
	if publisher == nil || len(repoIDs) == 0 {
		return nil
	}
	if err := retractStaleRepoReadinessPresence(ctx, presenceWriter, scopeID, generationID, repoIDs); err != nil {
		return err
	}
	states := repoReadinessPhaseStates(scopeID, generationID, repoIDs, observedAt)
	if len(states) == 0 {
		return nil
	}
	if err := publishGraphProjectionPhaseStatesWithRepair(ctx, publisher, repairQueue, states); err != nil {
		return fmt.Errorf("publish repo workload-materialization readiness phases: %w", err)
	}
	return nil
}

func publishRepoDependencyReadinessFenceWithRepair(
	ctx context.Context,
	publisher GraphProjectionPhasePublisher,
	repairQueue GraphProjectionPhaseRepairQueue,
	intent Intent,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	fence, _ := intent.Payload[RepoDependencyReadinessFencePayloadKey].(string)
	repoID, _ := intent.Payload[RepoDependencyReadinessRepoIDPayloadKey].(string)
	fence = strings.TrimSpace(fence)
	repoID = strings.TrimSpace(repoID)
	if fence == "" || repoID == "" {
		return nil
	}
	key := workloadMaterializationRepoReadinessKey(intent.ScopeID, repoID, intent.GenerationID)
	key.SourceRunID = RepoDependencyReadinessFenceSourceRunID(fence)
	if err := key.Validate(); err != nil {
		return fmt.Errorf("build repo dependency workload readiness fence: %w", err)
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	state := GraphProjectionPhaseState{
		Key:         key,
		Phase:       GraphProjectionPhaseWorkloadMaterialization,
		CommittedAt: observedAt.UTC(),
		UpdatedAt:   observedAt.UTC(),
	}
	if err := publishGraphProjectionPhaseStatesWithRepair(
		ctx,
		publisher,
		repairQueue,
		[]GraphProjectionPhaseState{state},
	); err != nil {
		return fmt.Errorf("publish repo dependency workload readiness fence: %w", err)
	}
	return nil
}

func enqueueRepoReadinessPhaseRepairs(
	ctx context.Context,
	repairQueue GraphProjectionPhaseRepairQueue,
	presenceWriter EndpointPresenceWriter,
	scopeID, generationID string,
	repoIDs []string,
	observedAt time.Time,
	cause error,
) error {
	if repairQueue == nil || len(repoIDs) == 0 {
		return nil
	}
	if err := retractStaleRepoReadinessPresence(ctx, presenceWriter, scopeID, generationID, repoIDs); err != nil {
		return err
	}
	states := repoReadinessPhaseStates(scopeID, generationID, repoIDs, observedAt)
	if len(states) == 0 {
		return nil
	}
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	repairs := gpphase.PhaseRepairsFromStates(states, reason, time.Now().UTC())
	if err := repairQueue.Enqueue(ctx, repairs); err != nil {
		return fmt.Errorf("enqueue repo workload-materialization readiness repairs: %w", err)
	}
	return nil
}

func retractStaleRepoReadinessPresence(
	ctx context.Context,
	presenceWriter EndpointPresenceWriter,
	scopeID, generationID string,
	repoIDs []string,
) error {
	if presenceWriter == nil {
		return nil
	}
	for _, keyspace := range repoReadinessPresenceKeyspaces {
		if err := presenceWriter.RetractStaleRepoGenerations(ctx, keyspace, scopeID, generationID, repoIDs); err != nil {
			return fmt.Errorf("retract stale %s presence before repo readiness: %w", keyspace, err)
		}
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

// loadScopeRepositoryFacts reads the scope generation's repository facts: the
// repo ids the zero-candidate path publishes readiness rows for (#2891), and
// the positive evidence of which repositories this generation fully covers
// for the stale repository-edge retract (#7285). The read is kind-filtered
// when the store implements FactKindLoader, as the Postgres FactStore does.
func loadScopeRepositoryFacts(ctx context.Context, loader FactLoader, scopeID, generationID string) ([]facts.Envelope, error) {
	if loader == nil {
		return nil, nil
	}
	envelopes, err := loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKindRepository})
	if err != nil {
		return nil, fmt.Errorf("load repository facts for workload materialization: %w", err)
	}
	return envelopes, nil
}

// retractStaleRepositoryEdges removes this domain's DEFINES and
// repository-side EXPOSES_ENDPOINT edges that the current full generation no
// longer supports (#7285, ruling B1). The projector used to wipe them as a side
// effect of deleting the Repository node on every non-delta attempt; it no
// longer does, because that delete also destroyed every other writer's edges.
// Handle calls it only after Materialize committed the current edges; a nil
// projection (the zero-candidate path) keeps nothing.
func (h WorkloadMaterializationHandler) retractStaleRepositoryEdges(
	ctx context.Context,
	intent Intent,
	repositoryFacts []facts.Envelope,
	projection *ProjectionResult,
) (retract.Result, error) {
	repoIDs := retract.FullGenerationRepositoryIDs(repositoryFacts)
	if len(repoIDs) == 0 {
		return retract.Result{}, nil
	}
	workloadIDs, endpointIDs := map[string][]string{}, map[string][]string{}
	if projection != nil {
		for _, row := range projection.WorkloadRows {
			workloadIDs[row.RepoID] = append(workloadIDs[row.RepoID], row.WorkloadID)
		}
		for _, row := range projection.EndpointRows {
			endpointIDs[row.RepoID] = append(endpointIDs[row.RepoID], row.EndpointID)
		}
	}
	keepLists := retract.KeepLists(repoIDs, workloadIDs, endpointIDs)
	started := time.Now()
	result, err := retract.RepositoryEdges(ctx, h.Materializer.executor, h.RepositoryEdgeReader,
		h.Materializer.batchSize(), keepLists, EvidenceSourceWorkloads)
	if err != nil {
		return result, err
	}
	retract.Observe(ctx, h.Instruments, intent.ScopeID, intent.GenerationID, keepLists, result, time.Since(started))
	return result, nil
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

// CypherGroupStatement is one statement in an atomic materializer write group.
type CypherGroupStatement struct {
	Cypher     string
	Parameters map[string]any
}

// CypherGroupExecutor executes all statements in one atomic graph transaction.
type CypherGroupExecutor interface {
	ExecuteCypherGroup(context.Context, []CypherGroupStatement) error
}

const batchRuntimePlatformRunsOnLegacyIdentityCleanupCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (p:Platform {id: row.platform_id})
MATCH (i)-[rel:RUNS_ON]->(p)
WHERE rel.identity_key IS NULL
DELETE rel`

// IsWorkloadRunsOnReplayGroup recognizes the materializer's exact atomic
// legacy-cleanup, keyed-identity, and owned-tuple write sequence. A failed
// commit rolls back all three statements; replaying the same rows then removes
// the same legacy identities and preserves a concurrent cross-repo tuple.
// Any changed template or row chunk must be reviewed before it can be retried.
func IsWorkloadRunsOnReplayGroup(group []CypherGroupStatement) bool {
	if len(group) != 3 ||
		group[0].Cypher != batchRuntimePlatformRunsOnLegacyIdentityCleanupCypher ||
		group[1].Cypher != batchRuntimePlatformRunsOnEdgeUpsertCypher ||
		group[2].Cypher != batchRuntimePlatformRunsOnOwnedEdgePropertiesCypher {
		return false
	}
	if len(group[0].Parameters) != 1 ||
		len(group[1].Parameters) != 1 ||
		len(group[2].Parameters) != 1 {
		return false
	}
	rows, ok := group[0].Parameters["rows"].([]map[string]any)
	if !ok || len(rows) == 0 ||
		!reflect.DeepEqual(rows, group[1].Parameters["rows"]) ||
		!reflect.DeepEqual(rows, group[2].Parameters["rows"]) {
		return false
	}
	seenPairs := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		for _, key := range []string{"instance_id", "platform_id", "evidence_source"} {
			value, ok := row[key].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return false
			}
		}
		pair := row["instance_id"].(string) + "\x00" + row["platform_id"].(string)
		if previous, exists := seenPairs[pair]; exists && !reflect.DeepEqual(previous, row) {
			return false
		}
		seenPairs[pair] = row
	}
	return true
}

func (m *WorkloadMaterializer) executeBatchedGroup(
	ctx context.Context,
	queries []string,
	rows []map[string]any,
) error {
	executor, ok := m.executor.(CypherGroupExecutor)
	if !ok {
		return fmt.Errorf("atomic Cypher group executor is required")
	}
	batchSize := m.batchSize()
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		params := map[string]any{"rows": rows[start:end]}
		statements := make([]CypherGroupStatement, 0, len(queries))
		for _, query := range queries {
			statements = append(statements, CypherGroupStatement{
				Cypher:     query,
				Parameters: params,
			})
		}
		if err := executor.ExecuteCypherGroup(ctx, statements); err != nil {
			return err
		}
	}
	return nil
}
