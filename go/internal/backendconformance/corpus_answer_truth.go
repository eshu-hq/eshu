// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

// The answer-truth cases cover the query shapes that returned wrong answers
// with no error on older NornicDB builds (#6689): the right number of rows
// with wrong values, which only an exact-row assertion can see. Each is the
// shape of a production read-model statement:
//
//   - count(DISTINCT) over a chain that reaches the same node on several paths
//     (graph-summary and repo-context platform_count, orneryd/NornicDB#362);
//   - collect(DISTINCT) over repeated values (catalog workload environments,
//     orneryd/NornicDB#368);
//   - a node-only anchor followed by two OPTIONAL MATCHes, projecting the
//     optional-bound nodes' properties (entity context and infra relationships,
//     orneryd/NornicDB#364, #369, #370);
//   - an OPTIONAL MATCH after a WITH that aggregates (repository graph
//     coverage).
//
// All four return the exact rows below on NornicDB v1.3.3 and on Neo4j. They
// run in the default corpora so a backend regression fails the blocking live
// conformance gate instead of reaching an API or MCP answer.

const (
	answerTruthRepoID       = "repo:backend-conformance"
	answerTruthWorkloadID   = "workload:backend-conformance:answer-truth"
	answerTruthInstanceOne  = "workload-instance:backend-conformance:answer-truth-1"
	answerTruthInstanceTwo  = "workload-instance:backend-conformance:answer-truth-2"
	answerTruthPlatformID   = "platform:backend-conformance:answer-truth"
	answerTruthWriteCase    = "answer-truth workload platform seed"
	answerTruthEnvironment  = "backend-conformance-prod"
	answerTruthInstanceType = "INSTANCE_OF"
)

// answerTruthReadCases returns the #6689 shape cases with their exact rows.
// The seed is one repository defining one workload with two instances, both in
// the same environment and both running on the same platform, so every
// aggregate below sees the same node or value more than once.
func answerTruthReadCases() []ReadCase {
	return []ReadCase{
		{
			Name:       "answer-truth count distinct over repeated paths",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
MATCH (i)-[:RUNS_ON]->(p:Platform)
RETURN count(DISTINCT p) AS platform_count`,
			Parameters: map[string]any{"repo_id": answerTruthRepoID},
			WantRows:   []map[string]any{{"platform_count": 1}},
		},
		{
			Name:       "answer-truth collect distinct over repeated values",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (w:Workload) WHERE w.id IN $ids
MATCH (inst:WorkloadInstance)-[:INSTANCE_OF]->(w)
RETURN w.id AS id,
       count(inst) AS instance_count,
       collect(DISTINCT inst.environment) AS environments`,
			Parameters: map[string]any{"ids": []string{answerTruthWorkloadID}},
			WantRows: []map[string]any{{
				"id":             answerTruthWorkloadID,
				"instance_count": 2,
				"environments":   []string{answerTruthEnvironment},
			}},
		},
		{
			Name:       "answer-truth second optional match after a node-only anchor",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (n) WHERE n.id = $id
OPTIONAL MATCH (n)-[outgoing]->(target)
OPTIONAL MATCH (source)-[incoming]->(n)
RETURN target.id AS target_id,
       type(incoming) AS incoming_type,
       source.id AS source_id`,
			Parameters: map[string]any{"id": answerTruthWorkloadID},
			WantRows: []map[string]any{
				{"target_id": nil, "incoming_type": "DEFINES", "source_id": answerTruthRepoID},
				{"target_id": nil, "incoming_type": answerTruthInstanceType, "source_id": answerTruthInstanceOne},
				{"target_id": nil, "incoming_type": answerTruthInstanceType, "source_id": answerTruthInstanceTwo},
			},
		},
		{
			Name:       "answer-truth optional match after an aggregating with",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (w:Workload {id: $id})
OPTIONAL MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
WITH w, count(DISTINCT i) AS instance_count
OPTIONAL MATCH (w)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:RUNS_ON]->(p:Platform)
RETURN instance_count, count(DISTINCT p) AS platform_count`,
			Parameters: map[string]any{"id": answerTruthWorkloadID},
			WantRows:   []map[string]any{{"instance_count": 2, "platform_count": 1}},
		},
	}
}

// answerTruthWriteCases seeds the graph the answer-truth read cases read back.
func answerTruthWriteCases() []WriteCase {
	upsert := func(cypher string, params map[string]any) sourcecypher.Statement {
		return sourcecypher.Statement{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: cypher, Parameters: params}
	}
	statements := []sourcecypher.Statement{
		upsert(`MERGE (r:Repository {id: $repo_id})`, map[string]any{"repo_id": answerTruthRepoID}),
		upsert(`MERGE (w:Workload {id: $workload_id})`, map[string]any{"workload_id": answerTruthWorkloadID}),
		upsert(`MERGE (p:Platform {id: $platform_id})`, map[string]any{"platform_id": answerTruthPlatformID}),
		upsert(`MATCH (r:Repository {id: $repo_id})
MATCH (w:Workload {id: $workload_id})
MERGE (r)-[:DEFINES]->(w)`, map[string]any{"repo_id": answerTruthRepoID, "workload_id": answerTruthWorkloadID}),
	}
	for _, instanceID := range []string{answerTruthInstanceOne, answerTruthInstanceTwo} {
		statements = append(statements,
			upsert(`MERGE (i:WorkloadInstance {id: $instance_id})
SET i.environment = $environment`, map[string]any{"instance_id": instanceID, "environment": answerTruthEnvironment}),
			upsert(`MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (w:Workload {id: $workload_id})
MERGE (i)-[:INSTANCE_OF]->(w)`, map[string]any{"instance_id": instanceID, "workload_id": answerTruthWorkloadID}),
			upsert(`MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (p:Platform {id: $platform_id})
MERGE (i)-[:RUNS_ON]->(p)`, map[string]any{"instance_id": instanceID, "platform_id": answerTruthPlatformID}),
		)
	}
	return []WriteCase{{
		Name:                  answerTruthWriteCase,
		Capability:            CapabilityCanonicalWrites,
		RequireAtomicGroup:    true,
		TransactionVisibility: "the repository-workload-instance-platform chain must commit together",
		Statements:            statements,
	}}
}
