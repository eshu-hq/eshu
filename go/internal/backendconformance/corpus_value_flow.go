// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The value-flow cloud sink cases pin the two statements the production loader
// runs (go/internal/reducer/code/value/cloud_sink_loader.go) and assert their
// exact rows on every backend the live conformance gate runs.
//
// History, so the shape of these cases makes sense. The loader used to run one
// statement that aggregated the workloads per (function, action) pair, kept the
// pairs with exactly one, took the workload with a subscript, and matched on to
// the cloud resources. It returned zero rows on NornicDB with no error, which
// silently emptied every value-flow cloud sink projection. On the pinned v1.3.3
// image the aggregation and subscript were fixed upstream, but
// `action.action IN sinkRel.actions` evaluated after the subscript-bound
// workload dropped every row, and the result also depended on the RETURN items
// (#6690). That pair ran here behind an opt-in with a CI gate that expected it
// to fail.
//
// The loader now reads raw (function, action, workload) rows, does the
// single-workload check in Go, and resolves the surviving pairs with an
// UNWIND-first chain of single-hop MATCHes. Both statements answer correctly on
// NornicDB v1.3.3 and on Neo4j, so they run in the default corpora, where a
// regression on either backend fails the blocking live-conformance gate.
//
// The cases assert exact rows, not a minimum count. A minimum count cannot tell
// a correct answer from one that also lets through the function that runs in two
// workloads, and that is exactly the wrong answer an earlier NornicDB build gave
// for this query.

// Case names. They are constants because the guards look the cases up by name.
const (
	valueFlowWorkloadRowsCaseName = "value-flow cloud action workload rows"
	valueFlowTargetsCaseName      = "value-flow cloud sink targets by pair"
	valueFlowWriteCaseName        = "value-flow cloud sink seed"
)

// valueFlowReadCases returns the read cases for the two production statements.
//
// The first reads the raw workload rows for three functions: one in a single
// workload, one in two workloads, and one whose action no principal is allowed.
// Every workload row must come back, including both rows for the two-workload
// function; excluding it is the loader's job, not the statement's.
//
// The second resolves the two pairs the loader would keep from those rows. Only
// the allowed action reaches the sink.
func valueFlowReadCases() []ReadCase {
	return []ReadCase{
		{
			Name:       valueFlowWorkloadRowsCaseName,
			Capability: CapabilityPathTraversal,
			Cypher:     reducer.ValueFlowCloudSinkWorkloadRowsCypher,
			Parameters: map[string]any{
				// []string, matching the production call site, which binds
				// functionUIDs[start:end].
				"function_uids": []string{valueFlowFunctionUID, valueFlowTwoWorkloadFunctionUID, valueFlowDeniedFunctionUID},
			},
			WantRows: []map[string]any{
				{"function_uid": valueFlowFunctionUID, "action": valueFlowAction, "workload_id": valueFlowWorkloadID},
				{"function_uid": valueFlowTwoWorkloadFunctionUID, "action": valueFlowAction, "workload_id": valueFlowWorkloadID},
				{"function_uid": valueFlowTwoWorkloadFunctionUID, "action": valueFlowAction, "workload_id": valueFlowSecondWorkloadID},
				{"function_uid": valueFlowDeniedFunctionUID, "action": valueFlowDeniedAction, "workload_id": valueFlowWorkloadID},
			},
		},
		{
			Name:       valueFlowTargetsCaseName,
			Capability: CapabilityPathTraversal,
			Cypher:     reducer.ValueFlowCloudSinkTargetsByPairCypher,
			Parameters: map[string]any{
				// []map[string]any, matching the production call site's
				// cloudSinkPairParams.
				"pairs": []map[string]any{
					{"function_uid": valueFlowFunctionUID, "action": valueFlowAction, "workload_id": valueFlowWorkloadID},
					{"function_uid": valueFlowDeniedFunctionUID, "action": valueFlowDeniedAction, "workload_id": valueFlowWorkloadID},
				},
			},
			WantRows: []map[string]any{
				{
					"function_uid":     valueFlowFunctionUID,
					"sink_rel":         "CAN_PERFORM",
					"sink_labels":      []string{"CloudResource"},
					"sink_is_internet": false,
				},
			},
		},
	}
}

// valueFlowWriteCases seeds the shape the read cases read back. The statements
// commit as one atomic group so a partial seed cannot be reported as a backend
// defect.
func valueFlowWriteCases() []WriteCase {
	statements := []sourcecypher.Statement{
		valueFlowFunction(valueFlowFunctionUID, "ExampleCloudCaller"),
		valueFlowFunction(valueFlowTwoWorkloadFunctionUID, "ExampleTwoWorkloadCaller"),
		valueFlowFunction(valueFlowDeniedFunctionUID, "ExampleDeniedCaller"),
		valueFlowUpsert(`MERGE (action:CloudAction {action: $action})`, map[string]any{"action": valueFlowAction}),
		valueFlowUpsert(`MERGE (action:CloudAction {action: $action})`, map[string]any{"action": valueFlowDeniedAction}),
		valueFlowWorkload(valueFlowWorkloadID, "backend-conformance-cloud-sink"),
		valueFlowWorkload(valueFlowSecondWorkloadID, "backend-conformance-cloud-sink-second"),
		valueFlowInvokes(valueFlowFunctionUID, valueFlowAction),
		valueFlowInvokes(valueFlowTwoWorkloadFunctionUID, valueFlowAction),
		valueFlowInvokes(valueFlowDeniedFunctionUID, valueFlowDeniedAction),
		valueFlowRunsIn(valueFlowFunctionUID, valueFlowWorkloadID),
		valueFlowRunsIn(valueFlowTwoWorkloadFunctionUID, valueFlowWorkloadID),
		valueFlowRunsIn(valueFlowTwoWorkloadFunctionUID, valueFlowSecondWorkloadID),
		valueFlowRunsIn(valueFlowDeniedFunctionUID, valueFlowWorkloadID),
		valueFlowUpsert(`MERGE (i:WorkloadInstance {id: $instance_id})
SET i.repo_id = $repo_id`, map[string]any{
			"instance_id": valueFlowInstanceID,
			"repo_id":     valueFlowRepoID,
		}),
		valueFlowUpsert(`MERGE (p:CloudResource {id: $principal_id})`, map[string]any{"principal_id": valueFlowPrincipalID}),
		valueFlowUpsert(`MERGE (s:CloudResource {id: $sink_id})
SET s.is_internet = false`, map[string]any{"sink_id": valueFlowSinkID}),
		valueFlowUpsert(`MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (w:Workload {id: $workload_id})
MERGE (i)-[:INSTANCE_OF]->(w)`, map[string]any{
			"instance_id": valueFlowInstanceID,
			"workload_id": valueFlowWorkloadID,
		}),
		valueFlowUpsert(`MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (p:CloudResource {id: $principal_id})
MERGE (i)-[:USES]->(p)`, map[string]any{
			"instance_id":  valueFlowInstanceID,
			"principal_id": valueFlowPrincipalID,
		}),
		// The allowed actions live on the relationship, as the production
		// graph stores them.
		valueFlowUpsert(`MATCH (p:CloudResource {id: $principal_id})
MATCH (s:CloudResource {id: $sink_id})
MERGE (p)-[rel:CAN_PERFORM]->(s)
SET rel.actions = $actions`, map[string]any{
			"principal_id": valueFlowPrincipalID,
			"sink_id":      valueFlowSinkID,
			"actions":      []any{valueFlowAction},
		}),
	}
	return []WriteCase{{
		Name:                  valueFlowWriteCaseName,
		Capability:            CapabilityCanonicalWrites,
		RequireAtomicGroup:    true,
		TransactionVisibility: "the whole function-to-sink chain must commit together",
		Statements:            statements,
	}}
}

func valueFlowUpsert(cypher string, params map[string]any) sourcecypher.Statement {
	return sourcecypher.Statement{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: cypher, Parameters: params}
}

func valueFlowFunction(uid, name string) sourcecypher.Statement {
	return valueFlowUpsert(`MERGE (fn:Function {uid: $function_uid})
SET fn.repo_id = $repo_id,
    fn.name = $function_name`, map[string]any{
		"function_uid":  uid,
		"function_name": name,
		"repo_id":       valueFlowRepoID,
	})
}

func valueFlowWorkload(id, name string) sourcecypher.Statement {
	return valueFlowUpsert(`MERGE (w:Workload {id: $workload_id})
SET w.name = $workload_name,
    w.repo_id = $repo_id`, map[string]any{
		"workload_id":   id,
		"workload_name": name,
		"repo_id":       valueFlowRepoID,
	})
}

func valueFlowInvokes(functionUID, action string) sourcecypher.Statement {
	return valueFlowUpsert(`MATCH (fn:Function {uid: $function_uid})
MATCH (action:CloudAction {action: $action})
MERGE (fn)-[:INVOKES_CLOUD_ACTION]->(action)`, map[string]any{"function_uid": functionUID, "action": action})
}

func valueFlowRunsIn(functionUID, workloadID string) sourcecypher.Statement {
	return valueFlowUpsert(`MATCH (fn:Function {uid: $function_uid})
MATCH (w:Workload {id: $workload_id})
MERGE (fn)-[:RUNS_IN]->(w)`, map[string]any{"function_uid": functionUID, "workload_id": workloadID})
}

// Fixture identifiers for the value-flow cases. They share the
// backend-conformance prefix every other case in this package uses so a run
// leaves one identifiable fixture set behind.
const (
	valueFlowFunctionUID            = "function:backend-conformance:cloud-caller"
	valueFlowTwoWorkloadFunctionUID = "function:backend-conformance:cloud-caller-two-workloads"
	valueFlowDeniedFunctionUID      = "function:backend-conformance:cloud-caller-denied"
	valueFlowWorkloadID             = "workload:backend-conformance:cloud-sink"
	valueFlowSecondWorkloadID       = "workload:backend-conformance:cloud-sink-second"
	valueFlowRepoID                 = "repo:backend-conformance-valueflow"
	valueFlowAction                 = "backend-conformance:GetObject"
	valueFlowDeniedAction           = "backend-conformance:SendMessage"
	valueFlowInstanceID             = "workload-instance:backend-conformance:cloud-sink"
	valueFlowPrincipalID            = "cloudresource:backend-conformance:principal"
	valueFlowSinkID                 = "cloudresource:backend-conformance:sink"
)
