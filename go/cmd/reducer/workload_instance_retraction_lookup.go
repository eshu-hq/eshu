// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// neo4jWorkloadInstanceRetractionLookup reads existing WorkloadInstance nodes
// so workload materialization can retract ids superseded by the current
// generation's projection (#5473: environment-alias canonicalization changes
// the durable instance id for repos whose overlay used a pre-canonical alias
// or non-canonical case).
type neo4jWorkloadInstanceRetractionLookup struct {
	reader query.GraphQuery
}

// ListWorkloadInstances returns the WorkloadInstance nodes owned by repoIDs and
// tagged with evidenceSource. It anchors on the workload_instance_repo_id index
// (graph/schema_tables.go: CREATE INDEX workload_instance_repo_id ... FOR
// (i:WorkloadInstance) ON (i.repo_id)), matching the same repo_id-scoped read
// path GraphServiceRuntimeInstanceLoader and neo4jWorkloadDependencyLookup use.
func (l neo4jWorkloadInstanceRetractionLookup) ListWorkloadInstances(
	ctx context.Context,
	repoIDs []string,
	evidenceSource string,
) ([]reducer.ExistingWorkloadInstance, error) {
	if l.reader == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	rows, err := l.reader.Run(ctx, `
		UNWIND $repo_ids AS repo_id
		MATCH (i:WorkloadInstance {repo_id: repo_id})
		WHERE i.evidence_source = $evidence_source
		RETURN DISTINCT i.repo_id AS repo_id, i.id AS instance_id
	`, map[string]any{
		"repo_ids":        repoIDs,
		"evidence_source": evidenceSource,
	})
	if err != nil {
		return nil, err
	}

	instances := make([]reducer.ExistingWorkloadInstance, 0, len(rows))
	for _, row := range rows {
		repoID := query.StringVal(row, "repo_id")
		instanceID := query.StringVal(row, "instance_id")
		if repoID == "" || instanceID == "" {
			continue
		}
		instances = append(instances, reducer.ExistingWorkloadInstance{
			RepoID:     repoID,
			InstanceID: instanceID,
		})
	}
	return instances, nil
}
