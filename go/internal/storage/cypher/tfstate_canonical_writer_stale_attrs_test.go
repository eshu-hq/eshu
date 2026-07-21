// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// fakeTerraformResourceGraph is a minimal in-memory node-property fixture
// scoped to the TerraformResource writer's own known statement shapes: a
// standalone `MATCH ... WHERE uid IN $uids REMOVE ...` statement (no rows
// parameter) and the UNWIND/MERGE/SET upsert statement with a trailing
// `r += row.attrs` map-merge (#5441 review round 9).
//
// IMPORTANT LIMITATION, found in review round 9: this fixture regex-extracts
// the REMOVE key list from the Cypher TEXT and applies it directly to a Go
// map. It never invokes NornicDB's parser or query router, so it proves the
// Go-level SEQUENCING this package's own code controls (REMOVE statement
// built and ordered before the upsert, with the right uids and property
// names) -- not that the pinned NornicDB executor actually parses and routes
// either statement shape the way this fixture assumes. An earlier version of
// the production fix (a REMOVE clause fused into the same MERGE...SET
// statement) passed this exact fixture green while corrupting
// r.evidence_source and leaving the stale attribute in place on the real
// backend -- see TestTerraformResourceWriterLiveClearsStaleAttributeOnRefresh
// in tfstate_canonical_writer_stale_attrs_live_test.go and
// docs/internal/evidence/5441-edge-node-properties.md for the backend-level
// proof this fixture cannot provide on its own.
type fakeTerraformResourceGraph struct {
	nodes map[string]map[string]any // uid -> properties
}

func newFakeTerraformResourceGraph() *fakeTerraformResourceGraph {
	return &fakeTerraformResourceGraph{nodes: map[string]map[string]any{}}
}

var fakeTerraformResourceGraphRemovePattern = regexp.MustCompile(`REMOVE ((?:r\.\w+(?:,\s*)?)+)`)

// Execute applies one statement to the in-memory node table. A statement
// carrying a "uids" parameter (the standalone REMOVE statement) deletes the
// named properties from each matched node. A statement carrying a "rows"
// parameter (the upsert) applies the row's fixed fields, then additively
// merges row.attrs -- matching the production Cypher clause order within
// that statement. The two are never combined in one statement in the
// production code as of #5441 review round 9.
func (g *fakeTerraformResourceGraph) Execute(_ context.Context, stmt Statement) error {
	if uids, ok := stmt.Parameters["uids"].([]string); ok {
		var removeKeys []string
		if m := fakeTerraformResourceGraphRemovePattern.FindStringSubmatch(stmt.Cypher); m != nil {
			for _, item := range regexp.MustCompile(`r\.(\w+)`).FindAllStringSubmatch(m[1], -1) {
				removeKeys = append(removeKeys, item[1])
			}
		}
		for _, uid := range uids {
			node := g.nodes[uid]
			if node == nil {
				continue
			}
			for _, key := range removeKeys {
				delete(node, key)
			}
		}
		return nil
	}

	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		return nil
	}
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		node := g.nodes[uid]
		if node == nil {
			node = map[string]any{}
			g.nodes[uid] = node
		}
		for k, v := range row {
			if k == "attrs" || k == "uid" {
				continue
			}
			node[k] = v
		}
		if attrs, ok := row["attrs"].(map[string]any); ok {
			for k, v := range attrs {
				node[k] = v
			}
		}
	}
	return nil
}

func baseTerraformStateResourceMat(attributes map[string]any) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      "tf-scope-stale",
		GenerationID: "tf-generation-stale",
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:              "tf-resource-stale-1",
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

// TestTerraformResourceWriterClearsStaleAttributeOnRefresh proves the
// intended Go-level sequencing for the real re-projection sequence a state
// refresh produces: project a resource WITH an allowlisted attribute, then
// re-project the SAME uid WITHOUT it, and assert the property is gone from
// the node afterward -- not just that the write succeeded once. See the
// fakeTerraformResourceGraph doc comment above for what this test does and
// does not prove.
func TestTerraformResourceWriterClearsStaleAttributeOnRefresh(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	graph := newFakeTerraformResourceGraph()

	// First projection: instance_type is present in state.
	matWith := baseTerraformStateResourceMat(map[string]any{
		"instance_type": "t3.micro",
		"ami":           "ami-0abcdef1234567890",
	})
	for _, stmt := range writer.buildTerraformStateStatements(matWith) {
		if err := graph.Execute(context.Background(), stmt); err != nil {
			t.Fatalf("first Execute() error = %v", err)
		}
	}
	node := graph.nodes["tf-resource-stale-1"]
	if got, want := node["tf_attr_instance_type"], "t3.micro"; got != want {
		t.Fatalf("after first projection tf_attr_instance_type = %#v, want %q", got, want)
	}

	// Second projection: the same resource, but instance_type has been
	// removed from state (the exact refresh scenario the additive merge
	// cannot handle by itself).
	matWithout := baseTerraformStateResourceMat(map[string]any{
		"ami": "ami-0abcdef1234567890",
	})
	for _, stmt := range writer.buildTerraformStateStatements(matWithout) {
		if err := graph.Execute(context.Background(), stmt); err != nil {
			t.Fatalf("second Execute() error = %v", err)
		}
	}
	node = graph.nodes["tf-resource-stale-1"]
	if _, stillPresent := node["tf_attr_instance_type"]; stillPresent {
		t.Fatalf("tf_attr_instance_type survived a refresh that no longer has it: node = %#v", node)
	}
	if got, want := node["tf_attr_ami"], "ami-0abcdef1234567890"; got != want {
		t.Fatalf("tf_attr_ami = %#v, want %q (attribute still in state must survive)", got, want)
	}
}

// TestTerraformResourceWriterClearsAllPromotedAttributesWhenNoneRemain
// covers the total-loss case: every previously-promoted attribute is gone
// from the refreshed state (not just one of several), so promotion yields no
// attrs at all on the second write.
func TestTerraformResourceWriterClearsAllPromotedAttributesWhenNoneRemain(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	graph := newFakeTerraformResourceGraph()

	matWith := baseTerraformStateResourceMat(map[string]any{
		"instance_type": "t3.micro",
		"ami":           "ami-0abcdef1234567890",
	})
	for _, stmt := range writer.buildTerraformStateStatements(matWith) {
		if err := graph.Execute(context.Background(), stmt); err != nil {
			t.Fatalf("first Execute() error = %v", err)
		}
	}

	// Refreshed state carries no allowlisted attributes for this resource
	// type at all (e.g. every attribute now exceeds the size cap, or the
	// resource's classified Attributes object came back empty).
	matEmpty := baseTerraformStateResourceMat(map[string]any{})
	for _, stmt := range writer.buildTerraformStateStatements(matEmpty) {
		if err := graph.Execute(context.Background(), stmt); err != nil {
			t.Fatalf("second Execute() error = %v", err)
		}
	}
	node := graph.nodes["tf-resource-stale-1"]
	if _, stillPresent := node["tf_attr_instance_type"]; stillPresent {
		t.Fatalf("tf_attr_instance_type survived a refresh with no attributes at all: node = %#v", node)
	}
	if _, stillPresent := node["tf_attr_ami"]; stillPresent {
		t.Fatalf("tf_attr_ami survived a refresh with no attributes at all: node = %#v", node)
	}
	// Fixed fields must still be current -- REMOVE must not wipe non-tf_attr_* properties.
	if got, want := node["address"], "aws_instance.web"; got != want {
		t.Fatalf("address = %#v, want %q", got, want)
	}
}

// TestTerraformStateStatementsEmitRemoveBeforeUpsert proves the statement
// ORDERING contract buildTerraformStateStatements' doc comment states:
// REMOVE statements must precede the upsert statement in the returned
// slice, never the reverse. This is a plain Cypher-text/shape assertion
// (no fake execution semantics involved), checking the one thing that must
// never regress silently: SET-then-REMOVE would wipe every write, not just
// refreshes.
func TestTerraformStateStatementsEmitRemoveBeforeUpsert(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := baseTerraformStateResourceMat(map[string]any{
		"instance_type": "t3.micro",
	})

	statements := writer.buildTerraformStateStatements(mat)
	sawRemove := false
	sawUpsert := false
	for _, stmt := range statements {
		if _, ok := stmt.Parameters["uids"]; ok {
			if sawUpsert {
				t.Fatalf("REMOVE statement found after the upsert statement; REMOVE must come first")
			}
			sawRemove = true
			continue
		}
		if _, ok := stmt.Parameters["rows"]; ok && stmt.Parameters[StatementMetadataEntityLabelKey] == "TerraformStateResource" {
			sawUpsert = true
		}
	}
	if !sawRemove {
		t.Fatalf("no REMOVE statement found for an allowlisted resource type")
	}
	if !sawUpsert {
		t.Fatalf("no upsert statement found")
	}
}
