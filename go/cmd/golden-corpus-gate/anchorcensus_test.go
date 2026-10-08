// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
)

func nodeStr(s string) *string { return &s }

// fakeCensusGraph classifies in-memory nodes with the reference definition,
// standing in for the Neo4j leg's graph after a replay.
type fakeCensusGraph struct {
	nodes     []anchor.Node
	err       error
	breakdown string
}

func (f fakeCensusGraph) AnchorCensus(context.Context) (anchor.Census, error) {
	if f.err != nil {
		return anchor.Census{}, f.err
	}
	uid, id := map[string]bool{}, map[string]bool{}
	for _, l := range anchor.UIDLabels() {
		uid[l] = true
	}
	for _, l := range anchor.IDLabels() {
		id[l] = true
	}
	var c anchor.Census
	for _, n := range f.nodes {
		switch anchor.Classify(n, uid, id) {
		case anchor.ReachViaID:
			c.IDBearing++
			c.ViaID++
		case anchor.ReachViaUID:
			c.IDBearing++
			c.ViaUIDOnly++
		case anchor.Unreachable:
			c.IDBearing++
			c.Residual++
		}
	}
	return c, nil
}

func (f fakeCensusGraph) ResidualLabelSets(context.Context) (string, error) { return f.breakdown, nil }

func cleanReplayNodes() []anchor.Node {
	return []anchor.Node{
		{Labels: []string{"Function"}, ID: nodeStr("f"), UID: nodeStr("f")},
		{Labels: []string{"Repository"}, ID: nodeStr("r")},
		{Labels: []string{"File"}, UID: nodeStr("file")},
	}
}

func censusFinding(t *testing.T, r Report) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Phase == "graph" && f.Check == "anchor_census" {
			return f
		}
	}
	t.Fatalf("no graph/anchor_census finding in %+v", r.Findings)
	return Finding{}
}

// GREEN: a clean Neo4j replay has no id-bearing node the anchor cannot reach.
func TestAnchorCensusCleanReplayPasses(t *testing.T) {
	var r Report
	checkAnchorCensus(context.Background(), fakeCensusGraph{nodes: cleanReplayNodes()}, true, &r)
	if f := censusFinding(t, r); !f.OK || !f.Required {
		t.Fatalf("clean replay finding = %+v, want required pass", f)
	}
}

// RED: one planted id-only node on an unconstrained label fails the gate and
// the finding names the label set without printing any id.
func TestAnchorCensusPlantedIDOnlyNodeFails(t *testing.T) {
	nodes := append(cleanReplayNodes(), anchor.Node{Labels: []string{"Unconstrained"}, ID: nodeStr("secret-entity-id")})
	var r Report
	checkAnchorCensus(context.Background(), fakeCensusGraph{nodes: nodes, breakdown: "labels=[Unconstrained] nodes=1"}, true, &r)
	f := censusFinding(t, r)
	if f.OK || !f.Required {
		t.Fatalf("planted node finding = %+v, want required failure", f)
	}
	if !strings.Contains(f.Detail, "labels=[Unconstrained] nodes=1") || strings.Contains(f.Detail, "secret-entity-id") {
		t.Fatalf("detail %q must name the label set and no id", f.Detail)
	}
}

func TestAnchorCensusFailsClosedWhenItCannotRun(t *testing.T) {
	var r Report
	checkAnchorCensus(context.Background(), fakeCensusGraph{err: errors.New("bolt down")}, true, &r)
	if f := censusFinding(t, r); f.OK {
		t.Fatalf("a census that could not run passed: %+v", f)
	}
}

// The labeled anchor exists only on Neo4j; the NornicDB loop still keeps the
// unlabeled fallback, so the census is skipped there and says so.
func TestAnchorCensusSkippedOffNeo4j(t *testing.T) {
	nodes := append(cleanReplayNodes(), anchor.Node{Labels: []string{"Unconstrained"}, ID: nodeStr("x")})
	var r Report
	checkAnchorCensus(context.Background(), fakeCensusGraph{nodes: nodes}, false, &r)
	f := censusFinding(t, r)
	if !f.OK || f.Required || !strings.Contains(f.Detail, "Neo4j") {
		t.Fatalf("off-Neo4j finding = %+v, want a non-required skip naming Neo4j", f)
	}
}
