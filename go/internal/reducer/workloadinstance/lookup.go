// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"fmt"
)

// GraphQueryRunner executes read-only graph queries. It is declared locally so
// this family never imports the reducer root; the concrete graph reader the
// reducer command wires satisfies it structurally.
type GraphQueryRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// GraphExistenceLookup implements ExistenceLookup against the canonical graph.
type GraphExistenceLookup struct {
	Graph GraphQueryRunner
}

// existingAnchorsCypher mirrors the endpoint MATCH of the USES writer
// (internal/storage/cypher/workload_cloud_relationship_writer.go), so "ready"
// means exactly "the writer's MATCH will bind". It is a read-only UNWIND whose
// RETURN aliases differ from the UNWIND binding, per the NornicDB
// variable-shadowing pitfall.
const existingAnchorsCypher = `UNWIND $anchors AS anchor
MATCH (workload:Workload {id: anchor.workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)
WHERE instance.environment = anchor.environment
RETURN workload.id AS ready_workload_id, instance.environment AS ready_environment`

// ExistingAnchors runs one bounded read over the given anchors.
func (l GraphExistenceLookup) ExistingAnchors(ctx context.Context, anchors []Anchor) (map[Anchor]struct{}, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	if l.Graph == nil {
		return nil, fmt.Errorf("graph workload instance existence lookup requires graph query runner")
	}
	params := make([]any, 0, len(anchors))
	for _, anchor := range anchors {
		params = append(params, map[string]any{"workload_id": anchor.WorkloadID, "environment": anchor.Environment})
	}
	rows, err := l.Graph.Run(ctx, existingAnchorsCypher, map[string]any{"anchors": params})
	if err != nil {
		return nil, fmt.Errorf("read existing workload instance anchors: %w", err)
	}
	existing := make(map[Anchor]struct{}, len(rows))
	for _, row := range rows {
		workloadID, _ := row["ready_workload_id"].(string)
		environment, _ := row["ready_environment"].(string)
		if workloadID != "" && environment != "" {
			existing[Anchor{WorkloadID: workloadID, Environment: environment}] = struct{}{}
		}
	}
	return existing, nil
}
