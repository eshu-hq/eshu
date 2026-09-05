// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"os"
	"strings"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// valueFlowCasesEnv opts the value-flow pair into the shared corpora.
//
// The pair reproduces defects that are open upstream, so it fails against
// NornicDB by design — that is the whole point of it. Left in the default
// corpora it would red-line the blocking live-conformance gate on every
// unrelated change until upstream lands a fix, which is a heavy toll for a
// defect already documented in five upstream issues and this package's
// evidence note.
//
// Off by default the pair is absent from the corpora entirely rather than
// present-and-skipped, so nothing runs it and nothing reports a false pass.
// Set it to run the pair on demand — which is how anyone checks whether
// upstream has fixed the underlying defects:
//
//	ESHU_BACKEND_CONFORMANCE_VALUE_FLOW=1 ESHU_BACKEND_CONFORMANCE_LIVE=1 \
//	  ESHU_GRAPH_BACKEND=nornicdb go test ./internal/backendconformance -run Live
//
// Nothing about the pair is weakened by this. With the variable set it runs
// exactly as before, and its failure still names the case.
const valueFlowCasesEnv = "ESHU_BACKEND_CONFORMANCE_VALUE_FLOW"

// valueFlowReadCaseName and valueFlowWriteCaseName identify the pair in the
// shared corpus. They are constants because the guards look the cases up by name.
const (
	valueFlowReadCaseName  = "value-flow cloud sink aggregation and subscript projection"
	valueFlowWriteCaseName = "value-flow cloud sink seed"
)

// valueFlowCasesEnabled reports whether the value-flow pair should be included
// in the shared corpora. It accepts the same truthy spellings as the live
// conformance opt-in so the two read consistently.
func valueFlowCasesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(valueFlowCasesEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// The cases in this file exist for one reason: the production value-flow cloud
// sink query returns zero rows on NornicDB and the correct row on Neo4j 5, and
// nothing in the repository detected that.
//
// valueFlowCloudSinkTargetsCypher
// (go/internal/reducer/valueflow/value_flow_cloud_sink_loader.go) resolves which cloud
// resources a function's cloud action can reach. Measured against Neo4j 5.x
// community and NornicDB (pinned build and upstream main), the statement
// diverges from Neo4j in the following ways:
//
//   - collect() after two MATCH clauses returns no rows at all on NornicDB,
//     where Neo4j returns the group.
//   - a subscript projection, `workloads[0] AS workload`, does not carry the
//     node through on NornicDB. It was measured by dereferencing a property --
//     `workload.name` returns the literal string "workload.name" there, where
//     Neo4j returns the value. The production statement does not dereference a
//     property off that binding; it uses it as the anchor of the next MATCH, so
//     the measured probe and the production use are related but not identical,
//     and "any one of these four empties the query" is established for the
//     other three rather than for this one.
//   - `action.action IN sinkRel.actions` matches nothing when the list lives on
//     a relationship, where Neo4j matches. The same predicate over a node list
//     property works on both, and the relationship property reads back fine.
//   - a MATCH clause with two or more relationship hops returns nothing when its
//     anchor was bound by an earlier clause. The statement's
//     `MATCH (workload)<-[:INSTANCE_OF]-(instance)-[:USES]->(principal)` is
//     exactly that shape, following a WITH-bound `workload`.
//
// Any one of the four empties the query, which is why this case runs the whole
// production statement rather than stopping at the first divergence. An earlier
// version stopped after the subscript projection; it would have gone green once
// the first two were fixed while production still returned nothing.
//
// Do not read the list as exhaustive. It grew from two to three to four as
// each round looked further along the statement, and every addition was found
// by testing past where the previous round stopped. The case asserts that the
// production statement returns a row on a conforming backend; it does not
// assert that these four are all the reasons it might not.
//
// The failure is silent: no error, no warning, and a graph that simply
// lacks the function-to-cloud-resource edges the query exists to produce. A
// backend that cannot serve this case cannot serve cloud value-flow reads, and
// this pair makes that fail loudly on the live conformance run instead of
// silently in projection.

// valueFlowReadCases returns the read half of the value-flow cloud sink
// conformance pair. It reproduces the production query's shape rather than
// paraphrasing it, so a backend fix or regression moves this case.
func valueFlowReadCases() []ReadCase {
	if !valueFlowCasesEnabled() {
		return nil
	}
	return []ReadCase{
		{
			Name:       valueFlowReadCaseName,
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
WHERE fn.uid IN $function_uids
MATCH (fn)-[:RUNS_IN]->(workload:Workload)
WITH fn, action, collect(DISTINCT workload) AS workloads
WHERE size(workloads) = 1
WITH fn, action, workloads[0] AS workload
MATCH (workload)<-[:INSTANCE_OF]-(instance:WorkloadInstance)-[:USES]->(principal:CloudResource)
MATCH (principal)-[sinkRel:CAN_PERFORM]->(sinkNode:CloudResource)
WHERE action.action IN sinkRel.actions
RETURN fn.uid AS function_uid,
       type(sinkRel) AS sink_rel,
       labels(sinkNode) AS sink_labels,
       sinkNode.is_internet AS sink_is_internet
ORDER BY function_uid, sink_rel`,
			Parameters: map[string]any{
				// []string, not []any: the production call site binds
				// functionUIDs[start:end], and divergence #3 is precisely that
				// IN over a list can match nothing on a non-conforming backend.
				// Handing the driver a different Go list type than production
				// would paraphrase the one thing this case exists to prove.
				"function_uids": []string{valueFlowFunctionUID},
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
	if !valueFlowCasesEnabled() {
		return nil
	}
	return []WriteCase{
		{
			Name:                  valueFlowWriteCaseName,
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: "the whole function-to-sink chain must commit together",
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
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (i:WorkloadInstance {id: $instance_id})
SET i.repo_id = $repo_id`,
					Parameters: map[string]any{
						"instance_id": valueFlowInstanceID,
						"repo_id":     valueFlowRepoID,
					},
				},
				{
					Operation:  sourcecypher.OperationCanonicalUpsert,
					Cypher:     `MERGE (p:CloudResource {id: $principal_id})`,
					Parameters: map[string]any{"principal_id": valueFlowPrincipalID},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (s:CloudResource {id: $sink_id})
SET s.is_internet = false`,
					Parameters: map[string]any{"sink_id": valueFlowSinkID},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (w:Workload {id: $workload_id})
MERGE (i)-[:INSTANCE_OF]->(w)`,
					Parameters: map[string]any{
						"instance_id": valueFlowInstanceID,
						"workload_id": valueFlowWorkloadID,
					},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (p:CloudResource {id: $principal_id})
MERGE (i)-[:USES]->(p)`,
					Parameters: map[string]any{
						"instance_id":  valueFlowInstanceID,
						"principal_id": valueFlowPrincipalID,
					},
				},
				{
					// The actions list lives on the relationship, which is the
					// shape the third divergence needs: `IN` over a list held on
					// an edge matches nothing on NornicDB.
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (p:CloudResource {id: $principal_id})
MATCH (s:CloudResource {id: $sink_id})
MERGE (p)-[rel:CAN_PERFORM]->(s)
SET rel.actions = $actions`,
					Parameters: map[string]any{
						"principal_id": valueFlowPrincipalID,
						"sink_id":      valueFlowSinkID,
						"actions":      []any{valueFlowAction},
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
	valueFlowRepoID      = "repo:backend-conformance-valueflow"
	valueFlowAction      = "backend-conformance:GetObject"
	valueFlowInstanceID  = "workload-instance:backend-conformance:cloud-sink"
	valueFlowPrincipalID = "cloudresource:backend-conformance:principal"
	valueFlowSinkID      = "cloudresource:backend-conformance:sink"
)
