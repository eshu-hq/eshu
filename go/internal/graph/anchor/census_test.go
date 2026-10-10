// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestClassify(t *testing.T) {
	uid := map[string]bool{"Function": true, "File": true}
	id := map[string]bool{"Repository": true, "Endpoint": true}
	tests := []struct {
		name string
		node Node
		want Reach
	}{
		{"uid label with uid equal to id", Node{Labels: []string{"Function"}, ID: strPtr("a"), UID: strPtr("a")}, ReachViaUID},
		{"id label without a uid", Node{Labels: []string{"Repository"}, ID: strPtr("a")}, ReachViaID},
		{"id label with a different uid", Node{Labels: []string{"Repository"}, ID: strPtr("a"), UID: strPtr("b")}, ReachViaID},
		{"uid label with a null uid is unreachable", Node{Labels: []string{"Function"}, ID: strPtr("a")}, Unreachable},
		{"uid label with a different uid is unreachable", Node{Labels: []string{"Function"}, ID: strPtr("a"), UID: strPtr("b")}, Unreachable},
		{"id only on an unconstrained label is unreachable", Node{Labels: []string{"Unconstrained"}, ID: strPtr("a")}, Unreachable},
		{"id on a node with no label is unreachable", Node{ID: strPtr("a")}, Unreachable},
		{"one constrained label among several is enough", Node{Labels: []string{"Unconstrained", "Function"}, ID: strPtr("a"), UID: strPtr("a")}, ReachViaUID},
		{"a node on both branches counts once, on the id branch", Node{Labels: []string{"Function", "Repository"}, ID: strPtr("a"), UID: strPtr("a")}, ReachViaID},
		{"a node without an id is not in the census", Node{Labels: []string{"File"}, UID: strPtr("a")}, NoID},
	}
	for _, tc := range tests {
		if got := Classify(tc.node, uid, id); got != tc.want {
			t.Errorf("%s: Classify = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseCensusRow(t *testing.T) {
	got, err := ParseCensusRow(map[string]any{
		"id_bearing": int64(898874), "via_id": int64(7911), "via_uid_only": int64(890963), "residual": int64(0),
	})
	if err != nil {
		t.Fatalf("ParseCensusRow: %v", err)
	}
	want := Census{IDBearing: 898874, ViaID: 7911, ViaUIDOnly: 890963, Residual: 0}
	if got != want {
		t.Fatalf("census = %+v, want %+v", got, want)
	}
	for _, row := range []map[string]any{nil, {"id_bearing": int64(1)}, {"id_bearing": "1", "via_id": int64(0), "via_uid_only": int64(0), "residual": int64(0)}} {
		if _, err := ParseCensusRow(row); err == nil {
			t.Errorf("ParseCensusRow(%v) accepted a malformed row", row)
		}
	}
}

func TestCensusParameters(t *testing.T) {
	params := CensusParameters()
	uid, id := params["uid_labels"].([]string), params["id_labels"].([]string)
	if len(uid) != len(UIDLabels()) || len(id) != len(IDLabels()) || len(uid) == 0 || len(id) == 0 {
		t.Fatalf("parameters carry %d uid and %d id labels, want the schema sets", len(uid), len(id))
	}
}

// memoryCensus runs the reference classification over in-memory nodes, so the
// gate verdict is exercised without a graph backend.
type memoryCensus struct {
	nodes []Node
	err   error
}

func (m memoryCensus) AnchorCensus(context.Context) (Census, error) {
	if m.err != nil {
		return Census{}, m.err
	}
	uid, id := map[string]bool{}, map[string]bool{}
	for _, l := range UIDLabels() {
		uid[l] = true
	}
	for _, l := range IDLabels() {
		id[l] = true
	}
	var c Census
	for _, n := range m.nodes {
		switch Classify(n, uid, id) {
		case ReachViaID:
			c.IDBearing++
			c.ViaID++
		case ReachViaUID:
			c.IDBearing++
			c.ViaUIDOnly++
		case Unreachable:
			c.IDBearing++
			c.Residual++
		}
	}
	return c, nil
}

func canonicalNodes() []Node {
	return []Node{
		{Labels: []string{"Function"}, ID: strPtr("f1"), UID: strPtr("f1")},
		{Labels: []string{"Repository"}, ID: strPtr("r1")},
		{Labels: []string{"File"}, UID: strPtr("file1")},
		{Labels: []string{"Directory"}},
	}
}

func TestEvaluateCensusPassesOnACleanReplay(t *testing.T) {
	verdict := EvaluateCensus(context.Background(), memoryCensus{nodes: canonicalNodes()})
	if !verdict.OK {
		t.Fatalf("clean replay failed: %s", verdict.Detail)
	}
	if !strings.Contains(verdict.Detail, "residual 0") {
		t.Fatalf("detail %q does not name the residual", verdict.Detail)
	}
}

func TestEvaluateCensusFailsOnAPlantedIDOnlyNode(t *testing.T) {
	nodes := append(canonicalNodes(), Node{Labels: []string{"Unconstrained"}, ID: strPtr("planted")})
	verdict := EvaluateCensus(context.Background(), memoryCensus{nodes: nodes})
	if verdict.OK {
		t.Fatal("a planted id-only node on an unconstrained label passed the census")
	}
	if !strings.Contains(verdict.Detail, "residual 1") {
		t.Fatalf("detail %q does not name the residual", verdict.Detail)
	}
}

func TestEvaluateCensusFailsOnAPlantedNullUIDNode(t *testing.T) {
	nodes := append(canonicalNodes(), Node{Labels: []string{"Function"}, ID: strPtr("planted")})
	if EvaluateCensus(context.Background(), memoryCensus{nodes: nodes}).OK {
		t.Fatal("a uid-labeled node with a null uid passed the census")
	}
}

func TestEvaluateCensusRefusesAnEmptyGraph(t *testing.T) {
	// A graph with no id-bearing node proves nothing: an empty projection
	// would otherwise pass vacuously.
	if EvaluateCensus(context.Background(), memoryCensus{}).OK {
		t.Fatal("an empty graph passed the census")
	}
}

func TestEvaluateCensusFailsWhenTheCensusCannotRun(t *testing.T) {
	verdict := EvaluateCensus(context.Background(), memoryCensus{err: errors.New("bolt down")})
	if verdict.OK || !strings.Contains(verdict.Detail, "bolt down") {
		t.Fatalf("verdict = %+v, want a failure naming the cause", verdict)
	}
}
