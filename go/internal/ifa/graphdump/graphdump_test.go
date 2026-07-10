// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"bytes"
	"context"
	"testing"
)

// fakeReader is an in-memory Reader used by every test in this package. It
// lets the canonicalizer be exercised hermetically, with no NornicDB/Neo4j
// dependency: the whole point of the Reader seam is that graphdump's logic
// is provable against a fake.
type fakeReader struct {
	nodes []Node
	edges []Edge
}

func (f fakeReader) Nodes(_ context.Context) ([]Node, error) { return f.nodes, nil }
func (f fakeReader) Edges(_ context.Context) ([]Edge, error) { return f.edges, nil }

// cloneProps returns a shallow copy of m so tests can mutate one graph's
// property map without aliasing another graph's "identical" input.
func cloneProps(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestCanonicalizeIsOrderIndependent(t *testing.T) {
	ctx := context.Background()

	repo := Node{Labels: []string{"Repository"}, Props: map[string]any{"uid": "repo-1"}}
	pkg := Node{Labels: []string{"Package"}, Props: map[string]any{"uid": "pkg-1"}}
	wf := Node{Labels: []string{"Workload"}, Props: map[string]any{"uid": "wf-1"}}

	dependsOn := Edge{
		Type:       "DEPENDS_ON",
		FromLabels: repo.Labels, FromProps: repo.Props,
		ToLabels: pkg.Labels, ToProps: pkg.Props,
		Props: map[string]any{"scope": "runtime"},
	}
	runsIn := Edge{
		Type:       "RUNS_IN",
		FromLabels: wf.Labels, FromProps: wf.Props,
		ToLabels: repo.Labels, ToProps: repo.Props,
		Props: map[string]any{"env": "prod"},
	}

	inOrder := fakeReader{
		nodes: []Node{repo, pkg, wf},
		edges: []Edge{dependsOn, runsIn},
	}
	shuffled := fakeReader{
		nodes: []Node{wf, repo, pkg},
		edges: []Edge{runsIn, dependsOn},
	}

	got1, err := Canonicalize(ctx, inOrder)
	if err != nil {
		t.Fatalf("Canonicalize(inOrder): %v", err)
	}
	got2, err := Canonicalize(ctx, shuffled)
	if err != nil {
		t.Fatalf("Canonicalize(shuffled): %v", err)
	}

	if !bytes.Equal(got1, got2) {
		t.Fatalf("Canonicalize is order-dependent:\n--- in-order ---\n%s\n--- shuffled ---\n%s", got1, got2)
	}
}

func TestCanonicalizeDetectsChangedNodeProperty(t *testing.T) {
	ctx := context.Background()

	base := fakeReader{
		nodes: []Node{
			{Labels: []string{"Repository"}, Props: map[string]any{"uid": "repo-1", "version": "1"}},
		},
	}
	changed := fakeReader{
		nodes: []Node{
			{Labels: []string{"Repository"}, Props: map[string]any{"uid": "repo-1", "version": "2"}},
		},
	}

	dBase, err := Digest(ctx, base)
	if err != nil {
		t.Fatalf("Digest(base): %v", err)
	}
	dChanged, err := Digest(ctx, changed)
	if err != nil {
		t.Fatalf("Digest(changed): %v", err)
	}

	if dBase == dChanged {
		t.Fatalf("Digest did not change when a node property changed: both %s", dBase)
	}
}

func TestCanonicalizeDetectsChangedEdge(t *testing.T) {
	ctx := context.Background()

	repo := Node{Labels: []string{"Repository"}, Props: map[string]any{"uid": "repo-1"}}
	pkgA := Node{Labels: []string{"Package"}, Props: map[string]any{"uid": "pkg-a"}}
	pkgB := Node{Labels: []string{"Package"}, Props: map[string]any{"uid": "pkg-b"}}

	baseline := fakeReader{
		nodes: []Node{repo, pkgA, pkgB},
		edges: []Edge{{
			Type:       "DEPENDS_ON",
			FromLabels: repo.Labels, FromProps: repo.Props,
			ToLabels: pkgA.Labels, ToProps: pkgA.Props,
		}},
	}
	dBaseline, err := Digest(ctx, baseline)
	if err != nil {
		t.Fatalf("Digest(baseline): %v", err)
	}

	t.Run("edge removed", func(t *testing.T) {
		g := fakeReader{nodes: baseline.nodes}
		d, err := Digest(ctx, g)
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		if d == dBaseline {
			t.Fatalf("removing an edge did not change the digest")
		}
	})

	t.Run("edge type changed", func(t *testing.T) {
		g := fakeReader{
			nodes: baseline.nodes,
			edges: []Edge{{
				Type:       "IMPORTS",
				FromLabels: repo.Labels, FromProps: repo.Props,
				ToLabels: pkgA.Labels, ToProps: pkgA.Props,
			}},
		}
		d, err := Digest(ctx, g)
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		if d == dBaseline {
			t.Fatalf("changing an edge's type did not change the digest")
		}
	})

	t.Run("edge endpoint changed", func(t *testing.T) {
		g := fakeReader{
			nodes: baseline.nodes,
			edges: []Edge{{
				Type:       "DEPENDS_ON",
				FromLabels: repo.Labels, FromProps: repo.Props,
				ToLabels: pkgB.Labels, ToProps: pkgB.Props,
			}},
		}
		d, err := Digest(ctx, g)
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		if d == dBaseline {
			t.Fatalf("changing an edge's target endpoint did not change the digest")
		}
	})
}

// TestContentAddressedEndpointsIgnoreInternalIDs proves the central design
// idea: an edge's endpoint identity comes from the endpoint's labels+props
// content, never from a backend element ID or struct/array identity. Neither
// Node nor Edge carries an id-like field at all (that is the Reader
// contract), so this models "two separate runs assigned different internal
// element IDs to the same logical node" by constructing the node's content
// twice, through two independent composite literals, and checking the
// digests still agree — and that an edge's computed endpoint digest matches
// the standalone node's own digest.
func TestContentAddressedEndpointsIgnoreInternalIDs(t *testing.T) {
	propsRun1 := map[string]any{"uid": "repo-1", "source_fact_id": "abc123"}
	propsRun2 := cloneProps(propsRun1)

	nodeRun1 := Node{Labels: []string{"Repository"}, Props: propsRun1}
	nodeRun2 := Node{Labels: []string{"Repository"}, Props: propsRun2}

	d1, err := nodeDigest(nodeRun1)
	if err != nil {
		t.Fatalf("nodeDigest(run1): %v", err)
	}
	d2, err := nodeDigest(nodeRun2)
	if err != nil {
		t.Fatalf("nodeDigest(run2): %v", err)
	}
	if d1 != d2 {
		t.Fatalf("nodeDigest is not content-addressed: run1=%s run2=%s", d1, d2)
	}

	edge := Edge{
		Type:       "DEPENDS_ON",
		FromLabels: nodeRun1.Labels, FromProps: nodeRun1.Props,
		ToLabels: []string{"Package"}, ToProps: map[string]any{"uid": "pkg-1"},
	}
	rec, err := edgeRecord(edge)
	if err != nil {
		t.Fatalf("edgeRecord: %v", err)
	}
	if rec["from"] != d1 {
		t.Fatalf("edge endpoint digest %v does not match the endpoint node's own digest %s", rec["from"], d1)
	}

	// Whole-graph level: two Readers built independently, with the same
	// content but representing what would be two different runs, must
	// canonicalize identically.
	ctx := context.Background()
	graphRun1 := fakeReader{nodes: []Node{nodeRun1}, edges: []Edge{edge}}
	graphRun2 := fakeReader{nodes: []Node{nodeRun2}, edges: []Edge{{
		Type:       "DEPENDS_ON",
		FromLabels: nodeRun2.Labels, FromProps: nodeRun2.Props,
		ToLabels: []string{"Package"}, ToProps: map[string]any{"uid": "pkg-1"},
	}}}
	same, err := Equal(ctx, graphRun1, graphRun2)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}
	if !same {
		t.Fatalf("two content-identical graphs from independent construction did not canonicalize identically")
	}
}

func TestOrphanMarkerNormalized(t *testing.T) {
	ctx := context.Background()

	base := fakeReader{nodes: []Node{
		{Labels: []string{"CloudResource"}, Props: map[string]any{
			"uid": "res-1", "eshu_orphan_observed_at_unix": float64(1690000000),
		}},
	}}
	sweptLater := fakeReader{nodes: []Node{
		{Labels: []string{"CloudResource"}, Props: map[string]any{
			"uid": "res-1", "eshu_orphan_observed_at_unix": float64(1700000000),
		}},
	}}

	dBase, err := Digest(ctx, base)
	if err != nil {
		t.Fatalf("Digest(base): %v", err)
	}
	dSwept, err := Digest(ctx, sweptLater)
	if err != nil {
		t.Fatalf("Digest(sweptLater): %v", err)
	}
	if dBase != dSwept {
		t.Fatalf("eshu_orphan_observed_at_unix was not normalized away: base=%s swept=%s", dBase, dSwept)
	}

	// Control: a non-denylisted property differing must still change the
	// digest, proving the denylist is not accidentally over-broad.
	controlChanged := fakeReader{nodes: []Node{
		{Labels: []string{"CloudResource"}, Props: map[string]any{
			"uid": "res-1", "eshu_orphan_observed_at_unix": float64(1690000000),
			"status": "orphaned",
		}},
	}}
	dControl, err := Digest(ctx, controlChanged)
	if err != nil {
		t.Fatalf("Digest(controlChanged): %v", err)
	}
	if dControl == dBase {
		t.Fatalf("a non-denylisted property difference was incorrectly normalized away")
	}
}
