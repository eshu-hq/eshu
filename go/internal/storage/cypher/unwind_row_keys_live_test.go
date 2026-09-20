// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live backend-parity proof for the #6782 missing-row-key audit: every writer
// the audit fixed leaves an optional property null on BOTH graph backends when
// its input has no value, and a re-projection clears a junk token an older
// writer already stored.
//
// The pinned NornicDB v1.3.3 stores the literal expression text for a missing
// UNWIND row key ("row.call_kind"), where Neo4j reads null. This test drives
// the production EdgeWriter (code calls, evidence artifacts, submodule pins)
// and CodeInterprocEvidenceWriter against a live backend and reads each
// optional property back with `IS NOT NULL`, the predicate readers use.
//
// Run it with the containers and flags documented in
// repo_dependency_source_tool_live_test.go, selecting this test:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:28000 go test ./internal/storage/cypher \
//	  -tags live_nornicdb_answer_truth -run TestLiveSparseWriterRowsLeaveOptionalPropertiesNull -count=1 -v
package cypher_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

const liveSparsePrefix = "live-6782-sparse:"

func TestLiveSparseWriterRowsLeaveOptionalPropertiesNull(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	live := openLiveSourceToolGraph(ctx, t)

	cleanup := `MATCH (n) WHERE n.id STARTS WITH '` + liveSparsePrefix + `' OR n.uid STARTS WITH '` + liveSparsePrefix + `' DETACH DELETE n`
	live.write(ctx, t, cleanup, nil)
	defer live.write(context.Background(), t, cleanup, nil)
	for _, id := range []string{"repo-a", "repo-b", "repo-c"} {
		live.write(ctx, t, `CREATE (:Repository {id: $id, name: $id})`, map[string]any{"id": liveSparsePrefix + id})
	}
	for _, uid := range []string{"fn-1", "fn-2", "fn-3", "fn-src", "fn-sink"} {
		live.write(ctx, t, `CREATE (:Function {uid: $uid, id: $uid, name: $uid})`, map[string]any{"uid": liveSparsePrefix + uid})
	}

	// Recovery case: an edge an older writer already stamped with the junk
	// token. The fixed writer's re-projection must clear it.
	live.write(ctx, t, `MATCH (s:Function {uid: $s}) MATCH (t:Function {uid: $t})
		MERGE (s)-[rel:CALLS]->(t) SET rel.call_kind = 'row.call_kind'`,
		map[string]any{"s": liveSparsePrefix + "fn-1", "t": liveSparsePrefix + "fn-3"})

	edges := edgewriter.NewEdgeWriter(live, 0)
	write := func(domain string, payloads ...map[string]any) {
		t.Helper()
		rows := make([]reducer.SharedProjectionIntentRow, 0, len(payloads))
		for index, payload := range payloads {
			rows = append(rows, reducer.SharedProjectionIntentRow{
				IntentID:     domain + string(rune('a'+index)),
				RepositoryID: liveSparsePrefix + "repo-a",
				GenerationID: "gen-1",
				Payload:      payload,
			})
		}
		if _, err := edges.WriteEdges(ctx, domain, rows, "test/live-sparse"); err != nil {
			t.Fatalf("WriteEdges(%s): %v", domain, err)
		}
	}
	write(reducer.DomainCodeCalls,
		map[string]any{"caller_entity_id": liveSparsePrefix + "fn-1", "callee_entity_id": liveSparsePrefix + "fn-2"},
		map[string]any{
			"caller_entity_id": liveSparsePrefix + "fn-1", "callee_entity_id": liveSparsePrefix + "fn-3",
			"caller_entity_type": "Function", "callee_entity_type": "Function",
		},
	)
	write(reducer.DomainRepoDependency, map[string]any{
		"repo_id": liveSparsePrefix + "repo-a", "target_repo_id": liveSparsePrefix + "repo-b",
		"relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-1",
		"evidence_artifacts": []any{map[string]any{"evidence_kind": "HELM_VALUES_REFERENCE", "path": "values.yaml"}},
	})
	write(reducer.DomainSubmodulePinEdges, map[string]any{
		"parent_repo_id": liveSparsePrefix + "repo-a", "resolved_repo_id": liveSparsePrefix + "repo-c",
		"submodule_path": "vendor/c",
	})
	interproc := sourcecypher.NewCodeInterprocEvidenceWriter(live, 0)
	if err := interproc.WriteCodeInterprocEvidence(ctx, []map[string]any{{
		"uid": liveSparsePrefix + "flow-1", "source_function_uid": liveSparsePrefix + "fn-src",
		"sink_function_uid": liveSparsePrefix + "fn-sink", "relative_path": "app/handler.go",
		"sink_kind": "sql", "source_kind": "http", "confidence": 0.9, "cloud": "",
	}}, "scope-1", "gen-1", "test/live-sparse"); err != nil {
		t.Fatalf("WriteCodeInterprocEvidence: %v", err)
	}

	checks := []struct {
		name     string
		cypher   string
		property string
	}{
		{
			"unlabelled CALLS call_kind",
			`MATCH (:Function {uid: $fn1})-[rel:CALLS]->(:Function {uid: $fn2}) RETURN rel.call_kind AS value`, "call_kind",
		},
		{
			"label-scoped CALLS call_kind (re-projected over a junk token)",
			`MATCH (:Function {uid: $fn1})-[rel:CALLS]->(:Function {uid: $fn3}) RETURN rel.call_kind AS value`, "call_kind",
		},
		{
			"EvidenceArtifact ref_value",
			`MATCH (:Repository {id: $ra})-[:HAS_DEPLOYMENT_EVIDENCE]->(a:EvidenceArtifact) RETURN a.ref_value AS value`, "ref_value",
		},
		{
			"EvidenceArtifact ref_pinned",
			`MATCH (:Repository {id: $ra})-[:HAS_DEPLOYMENT_EVIDENCE]->(a:EvidenceArtifact) RETURN a.ref_pinned AS value`, "ref_pinned",
		},
		{
			"EvidenceArtifact start_line",
			`MATCH (:Repository {id: $ra})-[:HAS_DEPLOYMENT_EVIDENCE]->(a:EvidenceArtifact) RETURN a.start_line AS value`, "start_line",
		},
		{
			"EvidenceArtifact commit_sha",
			`MATCH (:Repository {id: $ra})-[:HAS_DEPLOYMENT_EVIDENCE]->(a:EvidenceArtifact) RETURN a.commit_sha AS value`, "commit_sha",
		},
		{
			"PINS_SUBMODULE pinned_sha",
			`MATCH (:Repository {id: $ra})-[rel:PINS_SUBMODULE]->(:Repository {id: $rc}) RETURN rel.pinned_sha AS value`, "pinned_sha",
		},
		{
			"TAINT_FLOWS_TO why_trail_json",
			`MATCH (:Function {uid: $src})-[rel:TAINT_FLOWS_TO]->(:Function {uid: $sink}) RETURN rel.why_trail_json AS value`, "why_trail_json",
		},
		{
			"TAINT_FLOWS_TO why_trail_truncated",
			`MATCH (:Function {uid: $src})-[rel:TAINT_FLOWS_TO]->(:Function {uid: $sink}) RETURN rel.why_trail_truncated AS value`, "why_trail_truncated",
		},
	}
	readParams := map[string]any{
		"fn1": liveSparsePrefix + "fn-1", "fn2": liveSparsePrefix + "fn-2", "fn3": liveSparsePrefix + "fn-3",
		"src": liveSparsePrefix + "fn-src", "sink": liveSparsePrefix + "fn-sink",
		"ra": liveSparsePrefix + "repo-a", "rc": liveSparsePrefix + "repo-c",
	}
	for _, check := range checks {
		rows, err := live.Run(ctx, check.cypher, readParams)
		if err != nil {
			t.Fatalf("%s: read: %v", check.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: got %d rows on %s, want 1 (the writer did not project the element)", check.name, len(rows), live.backend)
		}
		t.Logf("%s %s: %s = %#v", live.backend, check.name, check.property, rows[0]["value"])
		if rows[0]["value"] != nil {
			t.Errorf("%s: %s = %#v on %s, want null (the input had no value)",
				check.name, check.property, rows[0]["value"], live.backend)
		}
	}
}
