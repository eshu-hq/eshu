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
// scoped to the TerraformResource writer's own known Cypher shape (MERGE by
// uid, a fixed-field SET clause, an optional static REMOVE clause, and a
// trailing `r += row.attrs` map-merge) -- the same "fake over the
// production writer's known shape, not a reimplementation of production
// logic" pattern fakeOrphanGraph uses in orphan_sweep_fixture_test.go. It
// exists because a single Cypher-text assertion cannot prove the additive
// `r += row.attrs` merge leaves a stale property on a second write (#5441
// review round 8, P1-a) -- that requires actually applying two sequential
// writes to node state and reading it back.
type fakeTerraformResourceGraph struct {
	nodes map[string]map[string]any // uid -> properties
}

func newFakeTerraformResourceGraph() *fakeTerraformResourceGraph {
	return &fakeTerraformResourceGraph{nodes: map[string]map[string]any{}}
}

var fakeTerraformResourceGraphRemovePattern = regexp.MustCompile(`REMOVE ((?:r\.\w+(?:,\s*)?)+)`)

// Execute applies one MERGE/SET/REMOVE/SET batch statement to the in-memory
// node table: REMOVE any statically-named properties the statement's Cypher
// text lists, apply the row's fixed fields, then additively merge row.attrs
// -- in that order, matching the production Cypher clause order.
func (g *fakeTerraformResourceGraph) Execute(_ context.Context, stmt Statement) error {
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		return nil
	}
	var removeKeys []string
	if m := fakeTerraformResourceGraphRemovePattern.FindStringSubmatch(stmt.Cypher); m != nil {
		for _, item := range regexp.MustCompile(`r\.(\w+)`).FindAllStringSubmatch(m[1], -1) {
			removeKeys = append(removeKeys, item[1])
		}
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
		for _, key := range removeKeys {
			delete(node, key)
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

// TestTerraformResourceWriterClearsStaleAttributeOnRefresh proves the real
// re-projection sequence a state refresh produces: project a resource WITH
// an allowlisted attribute, then re-project the SAME uid WITHOUT it, and
// assert the property is gone from the node afterward -- not just that the
// write succeeded once. Before the #5441 review round 8 fix, `r += row.attrs`
// was the only clause touching promoted attributes; an additive map-merge of
// an empty attrs map on the second write leaves the first write's value in
// place, so this test fails against the pre-fix Cypher (no REMOVE clause).
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
	// cannot handle).
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
