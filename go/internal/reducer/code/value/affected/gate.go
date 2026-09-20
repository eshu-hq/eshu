// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package affected

import (
	"context"
	"fmt"
)

// Runner executes read-only graph queries. It mirrors value.GraphQueryRunner,
// re-declared so this leaf never imports the reducer root; the same concrete
// implementation satisfies both structurally.
type Runner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// RefreshAffectedReposSignal is the Result.SubSignals key carrying the
// affected-repo count for the value-flow refresh ACK: the ACK emits a
// completion event only when CanonicalWrites is positive and this signal is
// absent (unwired producers fail open) or positive. Producers report an
// explicit zero when the gate runs empty so the ACK can tell "gated, none
// affected" from "gate not wired".
const RefreshAffectedReposSignal = "refresh_affected_repos"

// WithRefreshSignal sets RefreshAffectedReposSignal on an existing SubSignals
// map, allocating one when the input-invalid constructor returned nil for the
// zero case. The signal is always explicit (even zero) for the reason above.
func WithRefreshSignal(signals map[string]float64, count float64) map[string]float64 {
	if signals == nil {
		signals = make(map[string]float64, 1)
	}
	signals[RefreshAffectedReposSignal] = count
	return signals
}

// reposWithCloudCallersCypher lists the repos (from a candidate set) that own
// at least one Function calling a cloud action: the same
// INVOKES_CLOUD_ACTION + RUNS_IN shape CloudSinkWorkloadRowsCypher reads.
// Raw rows, deduped in Go: no aggregates, DISTINCT, WITH, or subscripts, per
// the NornicDB shapes #6690 misanswered.
//
// The UNWIND variable is deliberately NOT named repo_id: on NornicDB v1.3.3 a
// RETURN alias colliding with an UNWIND variable name resolves the output
// column to the variable's value instead of the alias (proven by
// TestLiveAffectedGate/repos failing with row keys keyed by repo id value).
const reposWithCloudCallersCypher = `UNWIND $repo_ids AS rid
MATCH (r:Repository {id: rid})
MATCH (r)-[:DEFINES]->(w:Workload)
MATCH (fn:Function)-[:RUNS_IN]->(w)
MATCH (fn)-[:INVOKES_CLOUD_ACTION]->(:CloudAction)
RETURN r.id AS repo_id`

// workloadReposWithCloudCallersCypher lists the repos defining workloads (from
// a candidate set) that own at least one Function calling a cloud action.
// The USES and aws_resource producers write rows anchored to workloads, so
// they resolve affected repos through DEFINES.
const workloadReposWithCloudCallersCypher = `UNWIND $workload_ids AS workload_id
MATCH (w:Workload {id: workload_id})
MATCH (r:Repository)-[:DEFINES]->(w)
MATCH (fn:Function)-[:RUNS_IN]->(w)
MATCH (fn)-[:INVOKES_CLOUD_ACTION]->(:CloudAction)
RETURN r.id AS repo_id`

// principalReposWithCloudCallersCypher lists the repos whose workloads run
// instances using any of the candidate principal resources and that own at
// least one Function calling a cloud action. The IAM CAN_PERFORM producer
// writes edges keyed by principal uid, so it resolves affected repos through
// the USES/INSTANCE_OF chain. Single-hop MATCHes only, UNWIND-first, like the
// proven CloudSinkTargetsByPairCypher chain.
const principalReposWithCloudCallersCypher = `UNWIND $principal_uids AS principal_uid
MATCH (p:CloudResource {uid: principal_uid})
MATCH (i:WorkloadInstance)-[:USES]->(p)
MATCH (i)-[:INSTANCE_OF]->(w:Workload)
MATCH (r:Repository)-[:DEFINES]->(w)
MATCH (fn:Function)-[:RUNS_IN]->(w)
MATCH (fn)-[:INVOKES_CLOUD_ACTION]->(:CloudAction)
RETURN r.id AS repo_id`

// ReposWithCloudCallers counts candidate repos owning at least one Function
// that calls a cloud action. Empty keys short-circuit to 0 without a query.
func ReposWithCloudCallers(ctx context.Context, g Runner, repoIDs []string) (int, error) {
	return countRepos(ctx, g, reposWithCloudCallersCypher, "repo_ids", repoIDs)
}

// ReposWithCloudCallersForWorkloads counts repos defining any of the workload
// ids and owning at least one Function that calls a cloud action.
func ReposWithCloudCallersForWorkloads(ctx context.Context, g Runner, workloadIDs []string) (int, error) {
	return countRepos(ctx, g, workloadReposWithCloudCallersCypher, "workload_ids", workloadIDs)
}

// ReposWithCloudCallersForPrincipals counts repos whose workloads run
// instances using any of the principal uids and owning at least one Function
// that calls a cloud action.
func ReposWithCloudCallersForPrincipals(ctx context.Context, g Runner, principalUIDs []string) (int, error) {
	return countRepos(ctx, g, principalReposWithCloudCallersCypher, "principal_uids", principalUIDs)
}

// ReposWithCloudCallersForResources counts repos whose workloads run
// instances using any of the resource uids and owning at least one Function
// that calls a cloud action. The aws_resource producer writes CloudResource
// nodes (not edges); a newly written node can only grow a sink through an
// instance USES edge, which is the same chain the principal statement reads,
// so this shares its statement under a resource-named entry point.
func ReposWithCloudCallersForResources(ctx context.Context, g Runner, resourceUIDs []string) (int, error) {
	return countRepos(ctx, g, principalReposWithCloudCallersCypher, "principal_uids", resourceUIDs)
}

func countRepos(
	ctx context.Context,
	g Runner,
	cypher, param string,
	keys []string,
) (int, error) {
	seen := make(map[string]struct{}, len(keys))
	uniq := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, key)
	}
	if len(uniq) == 0 {
		return 0, nil
	}
	rows, err := g.Run(ctx, cypher, map[string]any{param: uniq})
	if err != nil {
		return 0, fmt.Errorf("count repos with cloud callers: %w", err)
	}
	repos := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		repo, _ := row["repo_id"].(string)
		if repo == "" {
			continue
		}
		repos[repo] = struct{}{}
	}
	return len(repos), nil
}

// ShouldEmitRefresh decides whether a producer ACK emits a value-flow refresh
// completion event. CanonicalWrites must be positive (an empty run changes no
// cloud-sink inputs), and an explicit zero affected-repo signal suppresses the
// event ("gated, none affected"). An absent signal fails open so unwired
// producers still trigger the refresh rather than silently starving it.
func ShouldEmitRefresh(canonicalWrites int, signals map[string]float64) bool {
	if canonicalWrites <= 0 {
		return false
	}
	affected, ok := signals[RefreshAffectedReposSignal]
	if !ok {
		return true
	}
	return affected > 0
}
