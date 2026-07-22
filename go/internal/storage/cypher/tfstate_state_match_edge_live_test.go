// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// terraformStateConfigMatchCandidateCountCypherLiveTest mirrors
// cmd/projector's terraformStateConfigMatchCandidateCountCypher exactly (a
// single-clause UNWIND -> MATCH -> RETURN, proven safe against the pinned
// NornicDB backend; see that constant's doc comment for the WITH+collect()
// variant that silently dropped every row). Duplicated here rather than
// imported because cmd/projector (package main) cannot be imported by this
// package, and this package cannot import cmd/projector without an import
// cycle (cmd/projector already imports this package).
const terraformStateConfigMatchCandidateCountCypherLiveTest = `UNWIND $rows AS row
MATCH (c:TerraformResource {repo_id: row.owning_repo_id, name: row.address})
RETURN row.uid AS uid, count(c) AS candidate_count`

// boltConfigMatchResolver implements TerraformStateConfigMatchResolver
// against a real Bolt-connected backend via boltRetractTestRunner, using the
// exact same query shape production code runs
// (cmd/projector's projectorTerraformStateConfigMatchResolver).
type boltConfigMatchResolver struct {
	runner *boltRetractTestRunner
}

func (r boltConfigMatchResolver) CountConfigMatchCandidates(
	ctx context.Context,
	queries []TerraformStateConfigMatchQuery,
) (map[string]int, error) {
	rows := make([]map[string]any, len(queries))
	for i, q := range queries {
		rows[i] = map[string]any{"uid": q.UID, "owning_repo_id": q.OwningRepoID, "address": q.Address}
	}
	result, err := r.runner.runCypher(ctx, terraformStateConfigMatchCandidateCountCypherLiveTest, map[string]any{"rows": rows})
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(result))
	for _, row := range result {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		switch v := row["candidate_count"].(type) {
		case int64:
			counts[uid] = int(v)
		case float64:
			counts[uid] = int(v)
		}
	}
	return counts, nil
}

// TestCanonicalNodeWriterSkipsAmbiguousMatchesStateEdgeLive is the
// end-to-end real-backend regression test the #5443 P1 review finding
// required: "a regression test with two TerraformResource nodes sharing
// (repo_id, name) asserting the chosen behavior." Runs the FULL production
// path -- CanonicalNodeWriter.Write, which calls
// resolveTerraformStateConfigMatchAmbiguity then
// terraformStateMatchesConfigEdgeStatements -- against a real NornicDB (or
// Neo4j-compatible) backend seeded with a genuine ambiguous pair (two
// TerraformResource nodes in the same repo declaring the same address,
// which this repository's own schema permits: tf_resource_unique is (name,
// path, line_number), not (repo_id, name)) alongside an unambiguous control
// pair. Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestCanonicalNodeWriterSkipsAmbiguousMatchesStateEdgeLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		ambiguousRepoID  = "repo-5443-p1-live-ambiguous"
		uniqueRepoID     = "repo-5443-p1-live-unique"
		address          = "aws_instance.web"
		ambiguousStateID = "tf-5443-p1-live-state-ambiguous"
		uniqueStateID    = "tf-5443-p1-live-state-unique"
	)
	ctx := context.Background()

	cleanup := func() {
		if err := boltWriteStatement(
			context.Background(), runner,
			`MATCH (n) WHERE (n:TerraformResource AND n.repo_id IN $repo_ids) OR (n:TerraformStateResource AND n.uid IN $state_uids) DETACH DELETE n`,
			map[string]any{
				"repo_ids":   []string{ambiguousRepoID, uniqueRepoID},
				"state_uids": []string{ambiguousStateID, uniqueStateID},
			},
		); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	seedConfig := func(repoID, path string) {
		t.Helper()
		if err := boltWriteStatement(ctx, runner,
			`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: 1})`,
			map[string]any{"repo_id": repoID, "name": address, "path": path},
		); err != nil {
			t.Fatalf("seed TerraformResource repo_id=%s path=%s: %v", repoID, path, err)
		}
	}
	// Two Terraform roots in the same repo both declaring "aws_instance.web".
	seedConfig(ambiguousRepoID, "envs/a/main.tf")
	seedConfig(ambiguousRepoID, "envs/b/main.tf")
	// One Terraform root in a different repo: the unambiguous control.
	seedConfig(uniqueRepoID, "envs/a/main.tf")

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil).
		WithTerraformStateConfigMatchResolver(boltConfigMatchResolver{runner: runner})
	mat := projector.CanonicalMaterialization{
		ScopeID:         "tf-scope-5443-p1-live",
		GenerationID:    "tf-generation-5443-p1-live",
		FirstGeneration: true,
		TerraformStateResources: []projector.TerraformStateResourceRow{
			{
				UID: ambiguousStateID, Address: address, Mode: "managed", ResourceType: "aws_instance",
				Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
				OwningRepoID: ambiguousRepoID,
			},
			{
				UID: uniqueStateID, Address: address, Mode: "managed", ResourceType: "aws_instance",
				Name: "web", SourceConfidence: facts.SourceConfidenceObserved, CollectorKind: "terraform_state",
				OwningRepoID: uniqueRepoID,
			},
		},
	}

	if err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ambiguousEdges, err := runner.runCypher(
		ctx,
		`MATCH (c:TerraformResource {repo_id: $repo_id})-[e:MATCHES_STATE]->(s:TerraformStateResource {uid: $uid}) RETURN c.path AS path`,
		map[string]any{"repo_id": ambiguousRepoID, "uid": ambiguousStateID},
	)
	if err != nil {
		t.Fatalf("read ambiguous MATCHES_STATE edges: %v", err)
	}
	if len(ambiguousEdges) != 0 {
		t.Fatalf(
			"AMBIGUOUS MATCH FAILED CLOSED CHECK: got %d MATCHES_STATE edge(s) from the ambiguous (repo_id, name) pair, want 0 -- an ambiguous match must never silently fan an edge out to one of several candidates: %#v",
			len(ambiguousEdges), ambiguousEdges,
		)
	}

	uniqueEdges, err := runner.runCypher(
		ctx,
		`MATCH (c:TerraformResource {repo_id: $repo_id})-[e:MATCHES_STATE]->(s:TerraformStateResource {uid: $uid}) RETURN c.path AS path`,
		map[string]any{"repo_id": uniqueRepoID, "uid": uniqueStateID},
	)
	if err != nil {
		t.Fatalf("read unique MATCHES_STATE edge: %v", err)
	}
	if len(uniqueEdges) != 1 {
		t.Fatalf("unique (repo_id, name) pair MATCHES_STATE edges = %d, want 1 (the unambiguous control case must still resolve)", len(uniqueEdges))
	}
}
