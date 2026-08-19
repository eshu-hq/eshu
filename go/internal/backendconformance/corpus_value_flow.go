// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The cases in this file exist for one reason: the production value-flow cloud
// sink query returns zero rows on NornicDB and the correct row on Neo4j 5, and
// nothing in the repository detected that.
//
// valueFlowCloudSinkTargetsCypher
// (go/internal/reducer/value_flow_cloud_sink_loader.go) resolves which cloud
// resources a function's cloud action can reach. It combines four shapes: two
// MATCH clauses, an aggregation over them, a WITH-attached WHERE on the
// aggregate, and a subscript projection of a collected node. Measured against
// Neo4j 5.x community and NornicDB (pinned build and upstream main), two of
// those shapes diverge:
//
//   - collect() after two MATCH clauses returns no rows at all on NornicDB,
//     where Neo4j returns the group.
//   - `workloads[0] AS workload` followed by `workload.name` returns the
//     literal string "workload.name" on NornicDB, where Neo4j returns the
//     property value.
//
// Either alone empties the query. The failure is silent: no error, no warning,
// and a graph that simply lacks the function-to-cloud-resource edges the query
// exists to produce. A backend that cannot serve this case cannot serve cloud
// value-flow reads, and this pair makes that fail loudly on the live
// conformance run instead of silently in projection.

// valueFlowReadCases returns the read half of the value-flow cloud sink
// conformance pair. It reproduces the production query's shape rather than
// paraphrasing it, so a backend fix or regression moves this case.
func valueFlowReadCases() []ReadCase {
	return []ReadCase{
		{
			Name:       "value-flow cloud sink aggregation and subscript projection",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (fn:Function {uid: $function_uid})-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
MATCH (fn)-[:RUNS_IN]->(workload:Workload)
WITH fn, action, collect(DISTINCT workload) AS workloads
WHERE size(workloads) = 1
WITH fn, action, workloads[0] AS workload
RETURN fn.uid AS function_uid,
       action.action AS action,
       workload.name AS workload_name`,
			Parameters: map[string]any{
				"function_uid": valueFlowFunctionUID,
			},
			MinRows: 1,
		},
	}
}

// valueFlowWriteCases seeds the shape the read case above reads back: one
// Function that invokes one CloudAction and runs in exactly one Workload. The
// statements commit as one atomic group so a partial seed cannot be reported as
// a backend defect.
func valueFlowWriteCases() []WriteCase {
	return []WriteCase{
		{
			Name:                  "value-flow cloud sink seed",
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: "function, cloud action, workload and both edges must commit together",
			Statements: []sourcecypher.Statement{
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (fn:Function {uid: $function_uid})
SET fn.repo_id = $repo_id,
    fn.name = $function_name`,
					Parameters: map[string]any{
						"function_uid":  valueFlowFunctionUID,
						"function_name": "ExampleCloudCaller",
						"repo_id":       valueFlowRepoID,
					},
				},
				{
					Operation:  sourcecypher.OperationCanonicalUpsert,
					Cypher:     `MERGE (action:CloudAction {action: $action})`,
					Parameters: map[string]any{"action": valueFlowAction},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (w:Workload {id: $workload_id})
SET w.name = $workload_name,
    w.repo_id = $repo_id`,
					Parameters: map[string]any{
						"workload_id":   valueFlowWorkloadID,
						"workload_name": "backend-conformance-cloud-sink",
						"repo_id":       valueFlowRepoID,
					},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (fn:Function {uid: $function_uid})
MATCH (action:CloudAction {action: $action})
MERGE (fn)-[:INVOKES_CLOUD_ACTION]->(action)`,
					Parameters: map[string]any{
						"function_uid": valueFlowFunctionUID,
						"action":       valueFlowAction,
					},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (fn:Function {uid: $function_uid})
MATCH (w:Workload {id: $workload_id})
MERGE (fn)-[:RUNS_IN]->(w)`,
					Parameters: map[string]any{
						"function_uid": valueFlowFunctionUID,
						"workload_id":  valueFlowWorkloadID,
					},
				},
			},
		},
	}
}

// Fixture identifiers for the value-flow conformance pair. They share the
// backend-conformance prefix every other case in this package uses so a run
// leaves one identifiable fixture set behind.
const (
	valueFlowFunctionUID = "function:backend-conformance:cloud-caller"
	valueFlowWorkloadID  = "workload:backend-conformance:cloud-sink"
	valueFlowRepoID      = "repo:backend-conformance"
	valueFlowAction      = "backend-conformance:GetObject"
)
