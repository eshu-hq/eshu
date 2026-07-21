// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestTerraformResourceWriterLiveClearsStaleAttributeOnRefresh is the
// real-backend counterpart to TestTerraformResourceWriterClearsStaleAttributeOnRefresh
// (tfstate_canonical_writer_stale_attrs_test.go), required by #5441 review
// round 9's P0 finding: the fake in-memory fixture proved the intended
// SEQUENCING but never invoked NornicDB's parser or executor, so it stayed
// green while an earlier version of this fix corrupted r.evidence_source
// and left the stale attribute in place on the real pinned backend. This
// test runs the actual production Cypher (buildTerraformStateStatements)
// through a real Bolt-connected NornicDB (or Neo4j-compatible) backend via
// the shared boltRetractTestRunner/boltTestExecutor used elsewhere in this
// package's live suite.
//
// Opt-in, matching every other _live_test.go in this package: set
// ESHU_CYPHER_BOLT_DSN (and optionally ESHU_CYPHER_BOLT_DATABASE) to run it
// against a running graph backend. Skipped otherwise.
func TestTerraformResourceWriterLiveClearsStaleAttributeOnRefresh(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const uid = "tf-resource-5441-r9-live-1"
	ctx := context.Background()
	t.Cleanup(func() {
		if err := boltWriteStatement(
			context.Background(),
			runner,
			`MATCH (r:TerraformStateResource {uid: $uid}) DETACH DELETE r`,
			map[string]any{"uid": uid},
		); err != nil {
			t.Errorf("cleanup live terraform resource node: %v", err)
		}
	})

	writer := NewCanonicalNodeWriter(&boltTestExecutor{runner: runner}, 500, nil)

	mat := func(attributes map[string]any) projector.CanonicalMaterialization {
		return projector.CanonicalMaterialization{
			ScopeID:      "tf-scope-5441-r9-live",
			GenerationID: "tf-generation-5441-r9-live",
			TerraformStateResources: []projector.TerraformStateResourceRow{{
				UID:              uid,
				Address:          "aws_instance.web",
				Mode:             "managed",
				ResourceType:     "aws_instance",
				Name:             "web",
				SourceConfidence: facts.SourceConfidenceObserved,
				CollectorKind:    "terraform_state",
				Attributes:       attributes,
			}},
		}
	}

	// First projection: instance_type and ami both present in state.
	for _, stmt := range writer.buildTerraformStateStatements(mat(map[string]any{
		"instance_type": "t3.micro",
		"ami":           "ami-0abcdef1234567890",
	})) {
		if err := runner.runCypherSingle(ctx, stmt); err != nil {
			t.Fatalf("first projection statement failed: %v\ncypher: %s", err, stmt.Cypher)
		}
	}

	rows, err := runner.runCypher(ctx, `MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.tf_attr_instance_type AS instance_type, r.tf_attr_ami AS ami, r.evidence_source AS evidence_source`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read after first projection: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("read after first projection: got %d rows, want 1", len(rows))
	}
	if got, want := rows[0]["instance_type"], "t3.micro"; got != want {
		t.Fatalf("after first projection tf_attr_instance_type = %#v, want %q", got, want)
	}
	if got, want := rows[0]["evidence_source"], "projector/tfstate"; got != want {
		t.Fatalf("after first projection evidence_source = %#v, want %q (uncorrupted)", got, want)
	}

	// Second projection: same uid, instance_type has been removed from
	// state -- the exact refresh scenario the additive merge alone cannot
	// handle, and the exact scenario the fused REMOVE+SET shape corrupted.
	for _, stmt := range writer.buildTerraformStateStatements(mat(map[string]any{
		"ami": "ami-0abcdef1234567890",
	})) {
		if err := runner.runCypherSingle(ctx, stmt); err != nil {
			t.Fatalf("second projection statement failed: %v\ncypher: %s", err, stmt.Cypher)
		}
	}

	rows, err = runner.runCypher(ctx, `MATCH (r:TerraformStateResource {uid: $uid}) RETURN r.tf_attr_instance_type AS instance_type, r.tf_attr_ami AS ami, r.evidence_source AS evidence_source`, map[string]any{"uid": uid})
	if err != nil {
		t.Fatalf("read after second projection: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("read after second projection: got %d rows, want 1", len(rows))
	}

	// ASSERTION 1: the stale attribute is genuinely gone.
	if v, present := rows[0]["instance_type"]; present && v != nil {
		t.Fatalf("tf_attr_instance_type survived a refresh that no longer has it: %#v", v)
	}
	// tf_attr_ami must survive -- it is still in state.
	if got, want := rows[0]["ami"], "ami-0abcdef1234567890"; got != want {
		t.Fatalf("tf_attr_ami = %#v, want %q (attribute still in state must survive)", got, want)
	}
	// ASSERTION 2: evidence_source is exactly "projector/tfstate", not
	// corrupted with Cypher source text from a fused REMOVE clause.
	if got, want := rows[0]["evidence_source"], "projector/tfstate"; got != want {
		t.Fatalf("evidence_source = %#v, want %q (uncorrupted)", got, want)
	}
}
