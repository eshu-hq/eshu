// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// neo4jWorkloadSiblingLookup reads every Workload node sharing one of the
// given bare names so the workload materializer can pair cross-repository
// SAME_NAME edges in Go (#5385 section 1.3). The read anchors on the
// workload_name index (probe P2: NodeIndexSeek on pinned Neo4j).
type neo4jWorkloadSiblingLookup struct {
	reader query.GraphQuery
}

func (l neo4jWorkloadSiblingLookup) ListWorkloadSiblings(
	ctx context.Context,
	names []string,
) ([]reducer.WorkloadSibling, error) {
	if l.reader == nil || len(names) == 0 {
		return nil, nil
	}
	rows, err := l.reader.Run(ctx, `
		MATCH (o:Workload)
		WHERE o.name IN $names
		RETURN o.id AS id, o.name AS name, o.repo_id AS repo_id
	`, map[string]any{"names": names})
	if err != nil {
		return nil, err
	}
	siblings := make([]reducer.WorkloadSibling, 0, len(rows))
	for _, row := range rows {
		sibling := reducer.WorkloadSibling{
			ID:     query.StringVal(row, "id"),
			Name:   query.StringVal(row, "name"),
			RepoID: query.StringVal(row, "repo_id"),
		}
		if sibling.ID == "" || sibling.Name == "" || sibling.RepoID == "" {
			continue
		}
		siblings = append(siblings, sibling)
	}
	return siblings, nil
}
