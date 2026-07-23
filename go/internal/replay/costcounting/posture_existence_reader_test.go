// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import "context"

// echoAllExistenceReader answers every candidate uid a posture node writer's
// existence read asks about as already-existing. It satisfies
// cypher.PostureExistenceReader (issue #5652: the posture node writers now
// confirm a CloudResource uid exists via a separate read before writing, so
// MERGE never fabricates a node) and does not count as a write batch — the
// cost-counting scenarios in this package assert eshu_dp_neo4j_batches_executed_total
// off the write Executor only, which this reader is never wired into.
type echoAllExistenceReader struct{}

func (echoAllExistenceReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	candidates, _ := params["candidate_uids"].([]any)
	rows := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		if uid, ok := c.(string); ok && uid != "" {
			rows = append(rows, map[string]any{"existing_uid": uid})
		}
	}
	return rows, nil
}
