// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/semanticentity"
)

// nornicDBCommitUniqueConflictMessage is the commit-time UNIQUE conflict a
// concurrent canonical MERGE on the same uid produces on NornicDB. It is the
// error class ExecuteGroup absorbs by replaying the group.
const nornicDBCommitUniqueConflictMessage = "Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
	"(commit failed: constraint violation: " +
	"Constraint violation (UNIQUE on Variable.[uid]): " +
	"Node with uid=variable:test:1 already exists)"

// TestClassifyRetryableGraphWriteGroupErrorRetriesIdempotentRetractWithMerge
// pins the classifier contract that #6176 depends on: a group that mixes the
// semantic writer's predicate-scoped retract statements with MERGE upserts is
// replay-safe, so a commit-time UNIQUE conflict on it is retried in place
// rather than dead-lettered.
//
// Before #6176 the semantic retract was dispatched outside the group, so the
// group the classifier saw was all-MERGE. Folding the retract in made it
// mixed; without this contract the same race becomes terminal.
func TestClassifyRetryableGraphWriteGroupErrorRetriesIdempotentRetractWithMerge(t *testing.T) {
	t.Parallel()

	err := errors.New(nornicDBCommitUniqueConflictMessage)

	for name, stmts := range map[string][]Statement{
		"full repo retract then upsert": {
			{
				Operation: OperationCanonicalRetract,
				Cypher:    semanticEntityLabelRetractCypher("Variable"),
			},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
		"delta retract then upsert": {
			{
				Operation: OperationCanonicalRetract,
				Cypher:    semanticEntityDeltaLabelRetractCypher("Variable"),
			},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
		"canonical node clear then upsert": {
			{
				Operation: OperationCanonicalRetract,
				Cypher:    semanticEntityCanonicalNodeClearCypher("Variable", []string{"evidence_source", "name"}),
			},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
		"broad multi-label retract then upsert": {
			{Operation: OperationCanonicalRetract, Cypher: semanticEntityRetractCypher},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
		// A relationship retract writes its pattern with arrows. The `<` and
		// `>` in `-[rel]->` are not comparison operators, so the
		// parameter-binding check must not read them as one and fail-close
		// every edge retract in the repository.
		"relationship retract then upsert": {
			{
				Operation: OperationCanonicalRetract,
				Cypher: "MATCH (source:CloudResource)-[rel]->(:CloudResource)\n" +
					"WHERE rel.scope_id IN $scope_ids\n" +
					"  AND rel.evidence_source = $evidence_source\n" +
					"DELETE rel",
			},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
		// The reverse arrow form, for the same reason.
		"incoming relationship retract then upsert": {
			{
				Operation: OperationCanonicalRetract,
				Cypher: "MATCH (s:TerraformStateResource)<-[e:MATCHES_STATE]-(:TerraformResource)\n" +
					"WHERE s.scope_id = $scope_id AND s.uid IN $uids\n" +
					"DELETE e",
			},
			{
				Operation: OperationCanonicalUpsert,
				Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRetryableGraphWriteGroupError(err, stmts); got != graphWriteRetryReasonUniqueConflict {
				t.Fatalf("classify = %q, want %q", got, graphWriteRetryReasonUniqueConflict)
			}
		})
	}
}

// TestClassifyRetryableGraphWriteGroupErrorKeepsNonIdempotentGroupsTerminal
// keeps the widening narrow. Replaying a group is only safe when every
// statement converges; CREATE duplicates, an accumulating SET double-applies,
// and an UNWIND-driven delete is the NornicDB shape that no-ops inside a
// managed transaction rather than one whose predicates are stable.
func TestClassifyRetryableGraphWriteGroupErrorKeepsNonIdempotentGroupsTerminal(t *testing.T) {
	t.Parallel()

	err := errors.New(nornicDBCommitUniqueConflictMessage)
	upsert := Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    semanticEntityBatchedPropertiesUpsertCypher("Variable"),
	}

	for name, other := range map[string]Statement{
		"create": {
			Operation: OperationCanonicalUpsert,
			Cypher:    "MATCH (f:File {path: $path})\nCREATE (n:Audit {uid: $uid})",
		},
		"accumulating set": {
			Operation: OperationCanonicalUpsert,
			Cypher:    "MATCH (n:Variable {uid: $uid})\nSET n.write_count = n.write_count + 1",
		},
		"unwind driven delete": {
			Operation: OperationCanonicalRetract,
			Cypher:    "UNWIND $rows AS row\nMATCH (n:Variable {uid: row.uid})\nDETACH DELETE n",
		},
		"delete without a match": {
			Operation: OperationCanonicalRetract,
			Cypher:    "CALL db.doSomething()\nDETACH DELETE n",
		},
		"retract operation on a non-delete statement": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable {uid: $uid})\nRETURN n",
		},
		// The next two are fail-closed cases, not proven-unsafe ones. Both
		// would in fact converge on replay; the guard refuses them because
		// neither carries the two things the reasoning above rests on — a
		// statement the writer itself labels a retract, and Cypher that opens
		// on MATCH so its predicates come from the graph plus bound parameters
		// rather than from anything the group produced.
		"delete not labelled as a retract": {
			Operation: OperationCanonicalUpsert,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\nDETACH DELETE n",
		},
		"retract that does not open on MATCH": {
			Operation: OperationCanonicalRetract,
			Cypher:    "WITH $repo_ids AS ids\nMATCH (n:Variable)\nWHERE n.repo_id IN ids\nDETACH DELETE n",
		},
		// A retract that also writes is no longer a plain delete-by-predicate.
		// One case per clause the guard rejects, because a single
		// representative would leave the others unproven — dropping the clause
		// check entirely has to turn this table red.
		"retract that also creates": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\nDETACH DELETE n\nCREATE (t:Tombstone {uid: $uid})",
		},
		"retract that also accumulates a property": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\nSET n.retracted = n.retracted + 1\nDETACH DELETE n",
		},
		"retract driven by unwound rows": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (r:Repository {uid: $repo_uid})\nUNWIND $rows AS row\nMATCH (n:Variable {uid: row.uid})\nDETACH DELETE n",
		},
		"retract that calls a procedure": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\nCALL db.awaitIndexes()\nDETACH DELETE n",
		},
		"retract that deletes inside a foreach": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (r:Repository {uid: $repo_uid})\nFOREACH (n IN $nodes | DETACH DELETE n)",
		},
		// The open-ended-predicate cases. Every clause case above asks what
		// the statement WRITES; these ask what it MATCHES. A predicate that
		// selects the complement of a bound parameter, or that names no
		// parameter at all, grows its match set as concurrent writers commit,
		// so the replay can delete rows the failed attempt never saw. That
		// breaks the "removes the same parameter-bound set" premise the whole
		// widening rests on, so those retracts keep the group terminal.

		// The live shape: canonicalNodeRetractParametersCypher. Its
		// generation_id inequality matches every generation except this
		// writer's, so a Parameter committed by a concurrent writer on a
		// different generation between the failed attempt and the replay is
		// newly in range and is deleted.
		"retract whose predicate excludes a parameter instead of matching it": {
			Operation: OperationCanonicalRetract,
			Cypher: "MATCH (p:Parameter)\n" +
				"WHERE p.path IN $file_paths AND p.evidence_source = 'projector/canonical'\n" +
				"  AND p.generation_id <> $generation_id\n" +
				"DETACH DELETE p",
		},
		// Same class through negated membership rather than inequality.
		"retract whose predicate negates a parameter membership": {
			Operation: OperationCanonicalRetract,
			Cypher: "MATCH (d:Directory)\n" +
				"WHERE d.repo_id = $repo_id\n" +
				"  AND (d.path IS NULL OR NOT (d.path IN $directory_paths))\n" +
				"DETACH DELETE d",
		},
		// Same class through a coalesce-wrapped inequality, which the
		// Kubernetes namespace writer uses.
		"retract whose predicate wraps the inequality in coalesce": {
			Operation: OperationCanonicalRetract,
			Cypher: "MATCH (n:KubernetesNamespace {cluster_id: $cluster_id})\n" +
				"WHERE n.evidence_source = $evidence_source\n" +
				"  AND coalesce(n.generation_id, \"\") <> $generation_id\n" +
				"DETACH DELETE n",
		},
		// The extreme of the same class: a bare graph-state predicate binding
		// no parameter at all, so the match set is every node carrying the
		// flag whenever the statement happens to run.
		"retract whose predicate binds no parameter": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.stale\nDETACH DELETE n",
		},
		// The hybrid: a parameter IS named and no complement operator appears,
		// so the membership and open-ended checks both pass. The delete cannot
		// leave the key space $repo_ids enumerates, but within it the set still
		// moves -- a concurrent writer flipping n.stale between the failed
		// attempt and the replay puts a node in range the first attempt never
		// saw. Bounded blast radius, same broken "removes the same set" premise.
		// A second MATCH ... WHERE before the delete. whereClausePattern's
		// non-greedy body runs to the first write clause, so it swallows the
		// second pattern instead of yielding it separately -- the bare m.stale
		// would otherwise ride along inside a body holding $repo_ids.
		"retract whose second MATCH carries an unbounded WHERE": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\nMATCH (m:Other)\nWHERE m.stale\nDETACH DELETE n",
		},
		// No WHERE at all. boundParameterPattern sees $entity_ids and passes,
		// but a $param somewhere in the cypher says nothing about what the
		// match set is -- so this is refused rather than assumed bounded.
		"retract with no WHERE clause to verify": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nDETACH DELETE n\n// $entity_ids",
		},
		// OR WIDENS the set, so a term joined by it is more dangerous than an
		// AND'd one, not less: the parameter no longer bounds anything.
		"retract whose predicate ORs a graph-state term onto a bound membership": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\n  OR n.stale\nDETACH DELETE n",
		},
		// A literal cannot move, but the PROPERTY it is compared against can.
		// This is the same mutable read as the bare `n.stale` above.
		"retract comparing a mutable property against a literal": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\n  AND n.stale = true\nDETACH DELETE n",
		},
		"retract mixing a bound membership with a graph-state term": {
			Operation: OperationCanonicalRetract,
			Cypher:    "MATCH (n:Variable)\nWHERE n.repo_id IN $repo_ids\n  AND n.stale\nDETACH DELETE n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRetryableGraphWriteGroupError(err, []Statement{other, upsert}); got != "" {
				t.Fatalf("classify = %q, want \"\" (group must stay terminal)", got)
			}
		})
	}
}

// countingGroupExecutor fails ExecuteGroup failFor times with errMsg, then
// succeeds, recording the statements of the last call. Unlike
// failingGroupExecutor it keeps the group so a test can prove the production
// writer really emitted a mixed retract/upsert group.
type countingGroupExecutor struct {
	calls      atomic.Int32
	failFor    int
	errMsg     string
	lastGroup  []Statement
	groupCalls atomic.Int32
}

func (c *countingGroupExecutor) Execute(_ context.Context, _ Statement) error {
	c.calls.Add(1)
	return nil
}

func (c *countingGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	c.groupCalls.Add(1)
	c.lastGroup = append([]Statement(nil), stmts...)
	if int(c.groupCalls.Load()) <= c.failFor {
		return errors.New(c.errMsg)
	}
	return nil
}

// TestSemanticEntityWriterGroupedRetractConvergesOnCommitUniqueConflict is the
// #6176 regression: drive the production SemanticEntityWriter exactly as
// go/cmd/reducer/neo4j_wiring.go wires it for NornicDB, through the production
// RetryingExecutor, and fail the first grouped commit with the commit-time
// UNIQUE conflict a concurrent canonical MERGE on the same uid produces.
//
// The write must converge on replay instead of surfacing a terminal error that
// dead-letters the work item. Folding the retract into the upsert group made
// this group mixed; the test fails when the classifier only accepts all-MERGE
// groups.
func TestSemanticEntityWriterGroupedRetractConvergesOnCommitUniqueConflict(t *testing.T) {
	t.Parallel()

	for name, write := range map[string]semanticentity.SemanticEntityWrite{
		"full repo retract": {
			RepoIDs: []string{"repo:test:6176"},
			Rows: []semanticentity.SemanticEntityRow{{
				RepoID:       "repo:test:6176",
				EntityID:     "variable:test:1",
				EntityType:   "Variable",
				EntityName:   "@timeout",
				FilePath:     "/tmp/eshu-6176/lib/worker.ex",
				RelativePath: "lib/worker.ex",
				Language:     "elixir",
				StartLine:    2,
				EndLine:      2,
			}},
		},
		"delta retract": {
			RepoIDs:         []string{"repo:test:6176"},
			DeltaProjection: true,
			DeltaFilePaths:  []string{"/tmp/eshu-6176/lib/worker.ex"},
			Rows: []semanticentity.SemanticEntityRow{{
				RepoID:       "repo:test:6176",
				EntityID:     "variable:test:1",
				EntityType:   "Variable",
				EntityName:   "@timeout",
				FilePath:     "/tmp/eshu-6176/lib/worker.ex",
				RelativePath: "lib/worker.ex",
				Language:     "elixir",
				StartLine:    2,
				EndLine:      2,
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inner := &countingGroupExecutor{failFor: 1, errMsg: nornicDBCommitUniqueConflictMessage}
			retrying := &RetryingExecutor{Inner: inner, MaxRetries: 3, BaseDelay: time.Millisecond}
			writer := NewSemanticEntityWriterWithCanonicalNodeRows(retrying, 100).WithLabelScopedRetract()

			if _, err := writer.WriteSemanticEntities(context.Background(), write); err != nil {
				t.Fatalf("WriteSemanticEntities() error = %v, want nil (the group must replay to convergence)", err)
			}
			if got, want := int(inner.groupCalls.Load()), 2; got != want {
				t.Fatalf("ExecuteGroup calls = %d, want %d (1 conflict + 1 replay)", got, want)
			}

			// Guard the premise: the group really is mixed. If the writer ever
			// stops emitting the retract inside the group this test would pass
			// for the wrong reason.
			var retracts, merges int
			for _, stmt := range inner.lastGroup {
				if stmt.Operation == OperationCanonicalRetract {
					retracts++
				}
				if strings.Contains(strings.ToUpper(stmt.Cypher), "MERGE") {
					merges++
				}
			}
			if retracts == 0 {
				t.Fatalf("group carried %d retract statements, want at least 1 (the mixed-group premise is gone)", retracts)
			}
			if merges == 0 {
				t.Fatalf("group carried %d MERGE statements, want at least 1", merges)
			}
		})
	}
}
