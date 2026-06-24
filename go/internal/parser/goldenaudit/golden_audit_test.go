// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldenaudit

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareGraphPassesForExactFixtureTruth(t *testing.T) {
	t.Parallel()

	want := loadTestGolden(t, "python_service.json")
	got := Graph{
		Nodes: []Node{
			{ID: "function:app.handle", Kind: "function", Name: "handle", Path: "app.py", Line: 3},
			{ID: "function:app.persist", Kind: "function", Name: "persist", Path: "app.py", Line: 7},
		},
		Edges: []Edge{
			{SourceID: "function:app.handle", TargetID: "function:app.persist", Type: "CALLS"},
		},
	}

	report := CompareGraph(want, got)
	if !report.Pass() {
		t.Fatalf("CompareGraph() failed exact match: %s", report.Summary())
	}
}

func TestCompareGraphReportsMissingUnexpectedAndDuplicates(t *testing.T) {
	t.Parallel()

	want := loadTestGolden(t, "python_service.json")
	got := Graph{
		Nodes: []Node{
			{ID: "function:app.handle", Kind: "function", Name: "handle", Path: "app.py", Line: 3},
			{ID: "function:app.handle", Kind: "function", Name: "handle", Path: "app.py", Line: 3},
			{ID: "function:app.orphan", Kind: "function", Name: "orphan", Path: "app.py", Line: 12},
		},
		Edges: []Edge{
			{SourceID: "function:app.orphan", TargetID: "function:app.handle", Type: "CALLS"},
			{SourceID: "function:app.orphan", TargetID: "function:app.handle", Type: "CALLS"},
		},
	}

	report := CompareGraph(want, got)
	assertIDs(t, "missing nodes", report.MissingNodes, []string{"function:app.persist"})
	assertIDs(t, "unexpected nodes", report.UnexpectedNodes, []string{"function:app.orphan"})
	assertIDs(t, "duplicate observed nodes", report.DuplicateObservedNodes, []string{"function:app.handle"})
	assertEdgeKeys(t, "missing edges", report.MissingEdges, []string{"function:app.handle|CALLS|function:app.persist"})
	assertEdgeKeys(t, "unexpected edges", report.UnexpectedEdges, []string{"function:app.orphan|CALLS|function:app.handle"})
	assertEdgeKeys(t, "duplicate observed edges", report.DuplicateObservedEdges, []string{"function:app.orphan|CALLS|function:app.handle"})
	if report.Pass() {
		t.Fatalf("CompareGraph() passed drift report: %s", report.Summary())
	}
}

func TestCompareGraphSurfacesAccuracyForWrongTargetEdge(t *testing.T) {
	t.Parallel()

	want := loadTestGolden(t, "go_call_accuracy.json")
	got := Graph{
		Edges: []Edge{
			{SourceID: "func:server.Serve", TargetID: "func:server.handle", Type: "CALLS"},
			// Same source+type as golden's handle->Persist edge, but wrong target.
			{SourceID: "func:server.handle", TargetID: "func:store.Lookup", Type: "CALLS"},
		},
	}

	report := CompareGraph(want, got)
	if report.Accuracy.Overall.Precision >= 1.0 {
		t.Fatalf("Accuracy.Overall.Precision = %v, want < 1.0", report.Accuracy.Overall.Precision)
	}
	if !strings.Contains(report.Summary(), "accuracy_precision=") {
		t.Fatalf("Summary() missing accuracy precision: %s", report.Summary())
	}
}

func TestCompareGraphSecondLanguageFixtureAccuracy(t *testing.T) {
	t.Parallel()

	want := loadTestGolden(t, "js_call_accuracy.json")
	got := Graph{
		Edges: []Edge{
			// Correct edge.
			{SourceID: "func:router.route", TargetID: "func:router.dispatch", Type: "CALLS"},
			// Wrong target: shares source+type with golden's dispatch->save edge.
			{SourceID: "func:router.dispatch", TargetID: "func:repo.find", Type: "CALLS"},
		},
	}

	report := CompareGraph(want, got)
	if report.Accuracy.Overall.Precision != 0.5 {
		t.Fatalf("Accuracy.Overall.Precision = %v, want 0.5: %s", report.Accuracy.Overall.Precision, report.Summary())
	}
	if report.Accuracy.Overall.Recall != 0.5 {
		t.Fatalf("Accuracy.Overall.Recall = %v, want 0.5: %s", report.Accuracy.Overall.Recall, report.Summary())
	}
	assertEdgeKeys(t, "wrong-target edges", report.Accuracy.WrongTarget,
		[]string{"func:router.dispatch|CALLS|func:repo.find"})
}

func TestCompareGraphPassSemanticsUnchangedForExactMatch(t *testing.T) {
	t.Parallel()

	want := loadTestGolden(t, "js_call_accuracy.json")
	got := Graph{
		Nodes: []Node{
			{ID: "func:router.route", Kind: "function", Name: "route", Path: "src/router.js", Line: 5},
			{ID: "func:router.dispatch", Kind: "function", Name: "dispatch", Path: "src/router.js", Line: 18},
			{ID: "func:repo.save", Kind: "function", Name: "save", Path: "src/repo.js", Line: 9},
			{ID: "func:repo.find", Kind: "function", Name: "find", Path: "src/repo.js", Line: 24},
		},
		Edges: []Edge{
			{SourceID: "func:router.route", TargetID: "func:router.dispatch", Type: "CALLS"},
			{SourceID: "func:router.dispatch", TargetID: "func:repo.save", Type: "CALLS"},
		},
	}

	report := CompareGraph(want, got)
	if !report.Pass() {
		t.Fatalf("CompareGraph() failed structurally exact match: %s", report.Summary())
	}
	if report.Accuracy.Overall.Precision != 1.0 || report.Accuracy.Overall.Recall != 1.0 {
		t.Fatalf("exact-match accuracy = %v/%v, want 1.0/1.0",
			report.Accuracy.Overall.Precision, report.Accuracy.Overall.Recall)
	}
}

func TestLoadGoldenGraphRejectsDuplicateFixtureTruth(t *testing.T) {
	t.Parallel()

	_, err := LoadGoldenGraph(filepath.Join("testdata", "duplicate_truth.json"))
	if err == nil {
		t.Fatal("LoadGoldenGraph() error = nil, want duplicate truth failure")
	}
}

func TestLoadGoldenGraphRejectsIncompleteFixtureTruth(t *testing.T) {
	t.Parallel()

	_, err := LoadGoldenGraph(filepath.Join("testdata", "incomplete_truth.json"))
	if err == nil {
		t.Fatal("LoadGoldenGraph() error = nil, want incomplete truth failure")
	}
}

func TestLoadGoldenGraphRejectsDanglingFixtureEdges(t *testing.T) {
	t.Parallel()

	_, err := LoadGoldenGraph(filepath.Join("testdata", "dangling_edge.json"))
	if err == nil {
		t.Fatal("LoadGoldenGraph() error = nil, want dangling edge failure")
	}
}

func loadTestGolden(t *testing.T, name string) Graph {
	t.Helper()

	graph, err := LoadGoldenGraph(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("LoadGoldenGraph(%q) error = %v", name, err)
	}
	return graph
}

func assertIDs(t *testing.T, label string, nodes []Node, want []string) {
	t.Helper()

	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		got = append(got, node.ID)
	}
	assertStringSlices(t, label, got, want)
}

func assertEdgeKeys(t *testing.T, label string, edges []Edge, want []string) {
	t.Helper()

	got := make([]string, 0, len(edges))
	for _, edge := range edges {
		got = append(got, edge.Key())
	}
	assertStringSlices(t, label, got, want)
}

func assertStringSlices(t *testing.T, label string, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s len = %d (%v), want %d (%v)", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q; got all %v", label, i, got[i], want[i], got)
		}
	}
}
