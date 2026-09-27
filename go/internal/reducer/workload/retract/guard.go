// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Retract modes reported on Result.Mode and the completion log.
const (
	// ModeGuarded read the repositories' current targets first and deleted
	// only the stale ones, by id; no stale target means no DELETE statement.
	ModeGuarded = "guarded"
	// ModeUnguardedNoReader ran the keep-list deletes unconditionally
	// because no graph reader is wired.
	ModeUnguardedNoReader = "unguarded_no_reader"
	// ModeUnguardedReadFailed ran the keep-list deletes unconditionally
	// because the guard read failed (fail toward deleting).
	ModeUnguardedReadFailed = "unguarded_read_failed"
	// ModeSkippedNoScopeTruth sent no read and no delete because the caller
	// could not supply the scope generation's complete admitted set. A
	// keep-list is never built from an entity-filtered projection: that one
	// would delete the edges a sibling intent keyed to other entities wrote.
	// Only the caller chooses this mode; RepositoryEdges never does.
	ModeSkippedNoScopeTruth = "skipped_no_scope_truth"
)

// Reader is the graph read port for the guard: one parameterized read
// returning rows. It is the shape of query.GraphQuery.Run and of the
// reducer's shared graph query runner.
type Reader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// definesReadCypher lists the Workload ids a repository's $evidence_source
// DEFINES edges point at. Its MATCH/WHERE is definesCypher's without the
// keep-list, so the Go-side stale set answers the same question the
// unguarded delete does. The UNWIND binding is requested_repo_id, never
// repo_id: an UNWIND variable named like a RETURN alias mis-labels the
// returned column on NornicDB (#6786 shape X9).
const definesReadCypher = `UNWIND $repo_ids AS requested_repo_id
MATCH (repo:Repository {id: requested_repo_id})-[rel:DEFINES]->(w:Workload)
WHERE rel.evidence_source = $evidence_source
RETURN DISTINCT repo.id AS repo_id, w.id AS target_id`

// endpointReadCypher is definesReadCypher for the repository-side
// EXPOSES_ENDPOINT edge.
const endpointReadCypher = `UNWIND $repo_ids AS requested_repo_id
MATCH (repo:Repository {id: requested_repo_id})-[rel:EXPOSES_ENDPOINT]->(e:Endpoint)
WHERE rel.evidence_source = $evidence_source
RETURN DISTINCT repo.id AS repo_id, e.id AS target_id`

// guardedDefinesCypher deletes exactly the stale DEFINES edges the guard read
// found, anchored on both ids, and still scoped by $evidence_source at delete
// time so another writer's DEFINES to the same pair is never removed.
const guardedDefinesCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[rel:DEFINES]->(w:Workload {id: row.target_id})
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// guardedEndpointCypher is guardedDefinesCypher for the repository-side
// EXPOSES_ENDPOINT edge.
const guardedEndpointCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[rel:EXPOSES_ENDPOINT]->(e:Endpoint {id: row.target_id})
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// staleEdges holds the delete rows, one (repo_id, target_id) pair each.
type staleEdges struct {
	defines   []map[string]any
	endpoints []map[string]any
}

// readStaleEdges reads each repository's current DEFINES and repository-side
// EXPOSES_ENDPOINT targets and returns the ones outside its keep-list, sorted
// by repository then target. A row for a repository not in keepLists, or with
// a blank id, is ignored, so an over-broad read can never widen the delete.
func readStaleEdges(
	ctx context.Context,
	reader Reader,
	batchSize int,
	keepLists []KeepList,
	evidenceSource string,
) (staleEdges, error) {
	workloads := make(map[string]map[string]struct{}, len(keepLists))
	endpoints := make(map[string]map[string]struct{}, len(keepLists))
	repoIDs := make([]string, 0, len(keepLists))
	for _, keep := range keepLists {
		workloads[keep.RepoID] = toSet(keep.WorkloadIDs)
		endpoints[keep.RepoID] = toSet(keep.EndpointIDs)
		repoIDs = append(repoIDs, keep.RepoID)
	}
	defines, err := readStale(ctx, reader, batchSize, definesReadCypher, repoIDs, workloads, evidenceSource)
	if err != nil {
		return staleEdges{}, fmt.Errorf("read current workload defines edges: %w", err)
	}
	exposes, err := readStale(ctx, reader, batchSize, endpointReadCypher, repoIDs, endpoints, evidenceSource)
	if err != nil {
		return staleEdges{}, fmt.Errorf("read current repository endpoint edges: %w", err)
	}
	return staleEdges{defines: defines, endpoints: exposes}, nil
}

// readStale runs one existing-edge read in repository batches and keeps the
// (repo_id, target_id) pairs whose target is not in that repository's keep set.
func readStale(
	ctx context.Context,
	reader Reader,
	batchSize int,
	cypher string,
	repoIDs []string,
	keep map[string]map[string]struct{},
	evidenceSource string,
) ([]map[string]any, error) {
	seen := map[[2]string]struct{}{}
	for start := 0; start < len(repoIDs); start += batchSize {
		end := min(start+batchSize, len(repoIDs))
		rows, err := reader.Run(ctx, cypher, map[string]any{
			"repo_ids":        repoIDs[start:end],
			"evidence_source": evidenceSource,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			repoID, _ := row["repo_id"].(string)
			targetID, _ := row["target_id"].(string)
			repoID, targetID = strings.TrimSpace(repoID), strings.TrimSpace(targetID)
			kept, inScope := keep[repoID]
			if !inScope || targetID == "" {
				continue
			}
			if _, current := kept[targetID]; current {
				continue
			}
			seen[[2]string{repoID, targetID}] = struct{}{}
		}
	}
	pairs := make([][2]string, 0, len(seen))
	for pair := range seen {
		pairs = append(pairs, pair)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	stale := make([]map[string]any, 0, len(pairs))
	for _, pair := range pairs {
		stale = append(stale, map[string]any{"repo_id": pair[0], "target_id": pair[1]})
	}
	return stale, nil
}

// deleteStale deletes exactly the stale pairs the guard read found. A
// statement with no stale pair is not issued.
func deleteStale(
	ctx context.Context,
	executor Executor,
	batchSize int,
	stale staleEdges,
	evidenceSource string,
	result Result,
) (Result, error) {
	return runRetract(ctx, executor, batchSize, evidenceSource, result,
		retractStatement{cypher: guardedDefinesCypher, rows: stale.defines},
		retractStatement{cypher: guardedEndpointCypher, rows: stale.endpoints})
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
