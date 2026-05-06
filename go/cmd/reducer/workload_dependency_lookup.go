package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type neo4jWorkloadDependencyLookup struct {
	reader query.GraphQuery
}

func (l neo4jWorkloadDependencyLookup) ListRepoDependencyEdges(
	ctx context.Context,
	repoIDs []string,
) ([]reducer.RepoDependencyEdge, error) {
	if l.reader == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := l.reader.Run(ctx, `
		UNWIND $repo_ids AS repo_id
		MATCH (source:Repository {id: repo_id})-[:DEPENDS_ON]->(target:Repository)
		RETURN DISTINCT source.id AS source_repo_id, target.id AS target_repo_id
		UNION
		UNWIND $repo_ids AS repo_id
		MATCH (source:Repository)-[:DEPENDS_ON]->(target:Repository {id: repo_id})
		RETURN DISTINCT source.id AS source_repo_id, target.id AS target_repo_id
	`, map[string]any{"repo_ids": repoIDs})
	if err != nil {
		return nil, err
	}

	edges := make([]reducer.RepoDependencyEdge, 0, len(rows))
	for _, row := range rows {
		sourceRepoID := query.StringVal(row, "source_repo_id")
		targetRepoID := query.StringVal(row, "target_repo_id")
		if sourceRepoID == "" || targetRepoID == "" {
			continue
		}
		edges = append(edges, reducer.RepoDependencyEdge{
			SourceRepoID: sourceRepoID,
			TargetRepoID: targetRepoID,
		})
	}
	return edges, nil
}

func (l neo4jWorkloadDependencyLookup) ListRepoWorkloads(
	ctx context.Context,
	repoIDs []string,
) ([]reducer.RepoWorkload, error) {
	if l.reader == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := l.reader.Run(ctx, `
		MATCH (repo:Repository)-[:DEFINES]->(workload:Workload)
		WHERE repo.id IN $repo_ids
		RETURN DISTINCT repo.id AS repo_id, workload.id AS workload_id
	`, map[string]any{"repo_ids": repoIDs})
	if err != nil {
		return nil, err
	}

	workloads := make([]reducer.RepoWorkload, 0, len(rows))
	for _, row := range rows {
		repoID := query.StringVal(row, "repo_id")
		workloadID := query.StringVal(row, "workload_id")
		if repoID == "" || workloadID == "" {
			continue
		}
		workloads = append(workloads, reducer.RepoWorkload{
			RepoID:     repoID,
			WorkloadID: workloadID,
		})
	}
	return workloads, nil
}

// ListWorkloadDependencyEdges checks whether the workload dependency retract
// path has any current graph truth to remove for the requested repositories.
func (l neo4jWorkloadDependencyLookup) ListWorkloadDependencyEdges(
	ctx context.Context,
	repoIDs []string,
	evidenceSource string,
) ([]reducer.ExistingWorkloadDependencyEdge, error) {
	if l.reader == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := l.reader.Run(ctx, `
		MATCH (source:Workload)-[rel:DEPENDS_ON]->(target:Workload)
		WHERE source.repo_id IN $repo_ids
		  AND rel.evidence_source = $evidence_source
		RETURN DISTINCT source.repo_id AS repo_id
	`, map[string]any{
		"repo_ids":        repoIDs,
		"evidence_source": evidenceSource,
	})
	if err != nil {
		return nil, err
	}

	edges := make([]reducer.ExistingWorkloadDependencyEdge, 0, len(rows))
	for _, row := range rows {
		repoID := query.StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		edges = append(edges, reducer.ExistingWorkloadDependencyEdge{
			RepoID: repoID,
		})
	}
	return edges, nil
}
