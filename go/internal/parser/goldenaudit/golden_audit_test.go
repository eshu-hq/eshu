package goldenaudit

import (
	"path/filepath"
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
