// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"strings"
	"testing"
)

// TestBoltRetractCodeInterprocEvidenceByUIDs_Red reproduces the bug where
// RetractCodeInterprocEvidenceByUIDs passes []string parameters to the bolt
// driver and the edges survive even though the handler reports success.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. neo4j://127.0.0.1:17688).
// When unset the test skips.
func TestBoltRetractCodeInterprocEvidenceByUIDs_Red(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	const (
		testSourceUID   = "bolt-retract-test-src"
		testSinkUID     = "bolt-retract-test-sink"
		testScopeID     = "bolt-retract-test-scope"
		testEvidenceSrc = "reducer/code-interproc"
		testEdgeUID     = "bolt-retract-test-edge"
	)

	// 1. Seed Function nodes (these must exist; the writer MATCHes, not MERGEs).
	for _, uid := range []string{testSourceUID, testSinkUID} {
		if err := boltWriteStatement(
			ctx, runner,
			`MERGE (:Function {uid: $uid})`,
			map[string]any{"uid": uid},
		); err != nil {
			t.Fatalf("seed Function %q: %v", uid, err)
		}
	}

	// 2. Seed a TAINT_FLOWS_TO edge with the same properties the writer uses.
	if err := boltWriteStatement(
		ctx, runner,
		strings.Join([]string{
			`MATCH (s:Function {uid: $source_uid})`,
			`MATCH (t:Function {uid: $sink_uid})`,
			`MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: $evidence_uid}]->(t)`,
			`SET rel.scope_id = $scope_id,`,
			`rel.evidence_source = $evidence_source,`,
			`rel.generation_id = $generation_id`,
		}, " "),
		map[string]any{
			"source_uid":      testSourceUID,
			"sink_uid":        testSinkUID,
			"evidence_uid":    testEdgeUID,
			"scope_id":        testScopeID,
			"evidence_source": testEvidenceSrc,
			"generation_id":   "gen-1",
		},
	); err != nil {
		t.Fatalf("seed edge: %v", err)
	}

	// 3. Prove the edge exists before retract.
	g0, err := boltCount(
		ctx, runner,
		`MATCH (:Function {uid: $uid})-[rel:TAINT_FLOWS_TO]->(:Function) RETURN count(rel) AS count`,
		map[string]any{"uid": testSourceUID},
	)
	if err != nil {
		t.Fatalf("pre-retract count: %v", err)
	}
	if g0 == 0 {
		t.Fatalf("seed failed: 0 edges before retract")
	}
	t.Logf("pre-retract edge count: %d", g0)

	// 4. Retract through the REAL writer via the GroupExecutor dispatch path.
	//    This is the EXACT path the reducer uses.
	executor := &boltTestExecutor{runner: runner}
	writer := NewCodeInterprocEvidenceWriter(executor, 0)
	if err := writer.RetractCodeInterprocEvidenceByUIDs(
		ctx,
		[]string{testSourceUID},
		[]string{testScopeID},
		testEvidenceSrc,
	); err != nil {
		t.Fatalf("RetractCodeInterprocEvidenceByUIDs returned error: %v", err)
	}

	// 5. Prove the edge count after retract. THIS IS THE ASSERTION THAT MUST FAIL
	//    on the current buggy code.
	g1, err := boltCount(
		ctx, runner,
		`MATCH (:Function {uid: $uid})-[rel:TAINT_FLOWS_TO]->(:Function) RETURN count(rel) AS count`,
		map[string]any{"uid": testSourceUID},
	)
	if err != nil {
		t.Fatalf("post-retract count: %v", err)
	}
	if g1 != 0 {
		t.Fatalf("post-retract edge count = %d, want 0 — retract bug reproduced: []string params not correctly handled by bolt driver", g1)
	}
	t.Logf("post-retract edge count: %d (expected 0)", g1)
}

// TestBoltRetractCodeTaintEvidenceByUIDs_Red reproduces the same bug for the
// taint evidence writer's anchored retract.
func TestBoltRetractCodeTaintEvidenceByUIDs_Red(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	const (
		testFuncUID     = "bolt-taint-test-func"
		testNodeUID     = "bolt-taint-test-node"
		testScopeID     = "bolt-taint-test-scope"
		testEvidenceSrc = "reducer/code-taint"
	)

	// 1. Seed Function node.
	if err := boltWriteStatement(
		ctx, runner,
		`MERGE (:Function {uid: $uid})`,
		map[string]any{"uid": testFuncUID},
	); err != nil {
		t.Fatalf("seed Function: %v", err)
	}

	// 2. Seed a CodeTaintEvidence node attached to the Function.
	if err := boltWriteStatement(
		ctx, runner,
		strings.Join([]string{
			`MATCH (f:Function {uid: $func_uid})`,
			`MERGE (ev:CodeTaintEvidence {uid: $node_uid})`,
			`SET ev.scope_id = $scope_id,`,
			`ev.evidence_source = $evidence_source,`,
			`ev.generation_id = $generation_id`,
			`MERGE (f)-[:HAS_TAINT_EVIDENCE]->(ev)`,
		}, " "),
		map[string]any{
			"func_uid":        testFuncUID,
			"node_uid":        testNodeUID,
			"scope_id":        testScopeID,
			"evidence_source": testEvidenceSrc,
			"generation_id":   "gen-1",
		},
	); err != nil {
		t.Fatalf("seed taint node: %v", err)
	}

	// 3. Prove node exists.
	g0, err := boltCount(
		ctx, runner,
		`MATCH (n:CodeTaintEvidence {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": testNodeUID},
	)
	if err != nil {
		t.Fatalf("pre-retract count: %v", err)
	}
	if g0 == 0 {
		t.Fatalf("seed failed: 0 nodes before retract")
	}
	t.Logf("pre-retract node count: %d", g0)

	// 4. Retract through the REAL writer via GroupExecutor dispatch path.
	executor := &boltTestExecutor{runner: runner}
	writer := NewCodeTaintEvidenceWriter(executor, 0)
	if err := writer.RetractCodeTaintEvidenceByUIDs(
		ctx,
		[]string{testNodeUID},
		[]string{testScopeID},
		testEvidenceSrc,
	); err != nil {
		t.Fatalf("RetractCodeTaintEvidenceByUIDs returned error: %v", err)
	}

	// 5. Prove the node count after retract. MUST FAIL on current buggy code.
	g1, err := boltCount(
		ctx, runner,
		`MATCH (n:CodeTaintEvidence {uid: $uid}) RETURN count(n) AS count`,
		map[string]any{"uid": testNodeUID},
	)
	if err != nil {
		t.Fatalf("post-retract count: %v", err)
	}
	if g1 != 0 {
		t.Fatalf("post-retract node count = %d, want 0 — retract bug reproduced: []string params not correctly handled by bolt driver", g1)
	}
	t.Logf("post-retract node count: %d (expected 0)", g1)
}

// TestBoltUNWINDStringSliceIsolation isolates whether UNWIND with a []string
// parameter works over bolt. If UNWIND $uids AS uid where $uids is []string
// returns zero rows, the root cause is the parameter type.
func TestBoltUNWINDStringSliceIsolation(t *testing.T) {
	t.Parallel()

	runner := openBoltTestRunner(t)
	defer runner.close(context.Background())

	ctx := context.Background()

	// Try UNWIND with []string parameter.
	countStringSlice, err := boltCount(
		ctx, runner,
		`UNWIND $uids AS uid RETURN count(uid) AS count`,
		map[string]any{"uids": []string{"a", "b", "c"}},
	)
	if err != nil {
		t.Fatalf("UNWIND []string: %v", err)
	}
	t.Logf("UNWIND []string count = %d (expect 3)", countStringSlice)

	// Try UNWIND with []any parameter.
	countAnySlice, err := boltCount(
		ctx, runner,
		`UNWIND $uids AS uid RETURN count(uid) AS count`,
		map[string]any{"uids": []any{"a", "b", "c"}},
	)
	if err != nil {
		t.Fatalf("UNWIND []any: %v", err)
	}
	t.Logf("UNWIND []any count = %d (expect 3)", countAnySlice)

	// If []string returns 0 and []any returns 3, []string is the root cause.
	if countStringSlice == 0 && countAnySlice == 3 {
		t.Logf("ROOT CAUSE CONFIRMED: UNWIND with []string returns 0 rows over bolt; []any works correctly")
	}
}
