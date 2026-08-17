// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const rationaleRecordRepoID = "repository:r_fixture"

func TestRunAssertEdgesCommandDispatchesRationaleFullRecordComparison(t *testing.T) {
	expected := rationaleRecordFixture()
	raw, err := json.Marshal(struct {
		Edges []ifa.RationaleExpectedEdgeRecord `json:"edges"`
	}{Edges: expected})
	if err != nil {
		t.Fatalf("marshal expected fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "rationale-expected.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write expected fixture: %v", err)
	}

	actual := rationaleGraphEdges(expected)
	actual[0].Props["reason"] = "corrupted"
	edgeTypes, err := ifa.MaterializedEdgeDomainEdgeTypes("rationale_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(rationale_edges): %v", err)
	}
	identity, err := cypher.MaterializedEdgeIdentityProperties("rationale_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(rationale_edges): %v", err)
	}
	genericExpected := []ifa.ExpectedEdge{{
		RelationshipType: expected[0].RelationshipType,
		SourceEntityID:   expected[0].SourceEntityID,
		TargetEntityID:   expected[0].TargetEntityID,
	}}
	if err := assertMaterializedEdges(
		context.Background(), fakeEdgeReader{edges: actual}, "rationale_edges", edgeTypes, nil, identity, genericExpected,
	); err != nil {
		t.Fatalf("generic identity comparison rejected the property drift, so the dispatch regression is not discriminating: %v", err)
	}
	original := openAssertEdgesReader
	opened, closed := 0, 0
	openAssertEdgesReader = func(context.Context) (graphdump.Reader, func(), error) {
		opened++
		return fakeEdgeReader{edges: actual}, func() { closed++ }, nil
	}
	t.Cleanup(func() { openAssertEdgesReader = original })

	var stdout, stderr bytes.Buffer
	err = runAssertEdgesCommand(
		context.Background(),
		[]string{"-domain", "rationale_edges", "-expected", path},
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("runAssertEdgesCommand accepted a property-corrupted rationale edge; generic identity comparison was dispatched")
	}
	if !strings.Contains(err.Error(), "full materialized edge records do not match") {
		t.Fatalf("error = %q, want strict full-record rationale diagnostic", err)
	}
	if opened != 1 || closed != 1 {
		t.Fatalf("reader open/close calls = %d/%d, want 1/1", opened, closed)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no success report", stdout.String())
	}
}

func TestAssertRationaleMaterializedEdgeRecordsExactFullRecords(t *testing.T) {
	t.Parallel()
	fixturePath := filepath.Join(repoRootDir(t), "go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json")
	repoID, expected, err := ifa.LoadRationaleExpectedEdgeRecords(fixturePath)
	if err != nil {
		t.Fatalf("LoadRationaleExpectedEdgeRecords: %v", err)
	}
	if len(expected) != 3 {
		t.Fatalf("committed rationale record count = %d, want exact 3", len(expected))
	}
	actual := rationaleGraphEdges(expected)
	actual = append(actual, graphdump.Edge{
		Type:       "EXPLAINS",
		FromLabels: []string{"Rationale"},
		FromProps:  map[string]any{"uid": "rationale:foreign", "repo_id": "repository:foreign"},
		ToLabels:   []string{"Function"},
		ToProps:    map[string]any{"uid": "content-entity:foreign", "repo_id": "repository:foreign"},
		Props:      map[string]any{"evidence_source": "reducer/rationale"},
	})
	actual = append(actual, graphdump.Edge{
		Type:       "CONTAINS",
		FromLabels: []string{"Repository"},
		FromProps:  map[string]any{"id": repoID, "repo_id": repoID},
		ToLabels:   []string{"File"},
		ToProps:    map[string]any{"uid": "file:local", "repo_id": repoID},
	})

	err = assertRationaleMaterializedEdgeRecords(
		context.Background(), fakeEdgeReader{edges: actual}, repoID, expected,
	)
	if err != nil {
		t.Fatalf("assertRationaleMaterializedEdgeRecords(exact records plus foreign edge): %v", err)
	}
}

func TestAssertRationaleMaterializedEdgeRecordsSortsLabelsOnly(t *testing.T) {
	t.Parallel()
	expected := rationaleRecordFixture()
	actual := rationaleGraphEdges(expected)
	actual[0].FromLabels = []string{"Rationale", "Evidence"}
	actual[0].ToLabels = []string{"Function", "Code"}
	if err := assertRationaleMaterializedEdgeRecords(
		context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, expected,
	); err != nil {
		t.Fatalf("label order changed the record identity: %v", err)
	}
}

func TestAssertRationaleMaterializedEdgeRecordsRejectsMultisetAndRecordDrift(t *testing.T) {
	t.Parallel()
	expected := rationaleRecordFixture()
	base := rationaleGraphEdges(expected)

	tests := map[string]func([]graphdump.Edge) []graphdump.Edge{
		"missing": func(edges []graphdump.Edge) []graphdump.Edge { return edges[:len(edges)-1] },
		"extra": func(edges []graphdump.Edge) []graphdump.Edge {
			extra := cloneGraphEdge(t, edges[0])
			extra.FromProps["uid"] = "rationale:extra"
			return append(edges, extra)
		},
		"duplicate": func(edges []graphdump.Edge) []graphdump.Edge { return append(edges, cloneGraphEdge(t, edges[0])) },
		"cross-repo target": func(edges []graphdump.Edge) []graphdump.Edge {
			extra := cloneGraphEdge(t, edges[0])
			extra.FromProps["repo_id"] = "repository:foreign"
			extra.FromProps["uid"] = "rationale:foreign"
			return append(edges, extra)
		},
		"source property": func(edges []graphdump.Edge) []graphdump.Edge {
			edges[0].FromProps["excerpt_hash"] = "corrupted"
			return edges
		},
		"source label": func(edges []graphdump.Edge) []graphdump.Edge {
			edges[0].FromLabels = []string{"Corrupted"}
			return edges
		},
		"source property added":   func(edges []graphdump.Edge) []graphdump.Edge { edges[0].FromProps["extra"] = true; return edges },
		"source property removed": func(edges []graphdump.Edge) []graphdump.Edge { delete(edges[0].FromProps, "type"); return edges },
		"edge property":           func(edges []graphdump.Edge) []graphdump.Edge { edges[0].Props["reason"] = "corrupted"; return edges },
		"edge property added":     func(edges []graphdump.Edge) []graphdump.Edge { edges[0].Props["extra"] = true; return edges },
		"edge property removed":   func(edges []graphdump.Edge) []graphdump.Edge { delete(edges[0].Props, "confidence"); return edges },
		"target property": func(edges []graphdump.Edge) []graphdump.Edge {
			edges[0].ToProps["language"] = "corrupted"
			return edges
		},
		"target label":            func(edges []graphdump.Edge) []graphdump.Edge { edges[0].ToLabels = []string{"Class"}; return edges },
		"target property added":   func(edges []graphdump.Edge) []graphdump.Edge { edges[0].ToProps["extra"] = true; return edges },
		"target property removed": func(edges []graphdump.Edge) []graphdump.Edge { delete(edges[0].ToProps, "end_line"); return edges },
	}

	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := cloneGraphEdges(t, base)
			actual = mutate(actual)
			err := assertRationaleMaterializedEdgeRecords(
				context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, expected,
			)
			if err == nil {
				t.Fatal("assertRationaleMaterializedEdgeRecords = nil error, want exact-record mismatch")
			}
			if !strings.Contains(err.Error(), "full materialized edge records do not match") {
				t.Errorf("error = %q, want exact-record mismatch diagnostic", err)
			}
		})
	}
}

func TestAssertRationaleMaterializedEdgeRecordsRejectsExpectedIdentityMovedToForeignRepo(t *testing.T) {
	t.Parallel()
	expected := rationaleRecordFixture()
	tests := map[string]func(graphdump.Edge) graphdump.Edge{
		"foreign repository values": func(edge graphdump.Edge) graphdump.Edge {
			edge.FromProps["repo_id"] = "repository:foreign"
			edge.ToProps["repo_id"] = "repository:foreign"
			return edge
		},
		"missing repository values": func(edge graphdump.Edge) graphdump.Edge {
			delete(edge.FromProps, "repo_id")
			delete(edge.ToProps, "repo_id")
			return edge
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := rationaleGraphEdges(expected)
			actual = append(actual, mutate(cloneGraphEdge(t, actual[0])))
			err := assertRationaleMaterializedEdgeRecords(
				context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, expected,
			)
			if err == nil {
				t.Fatal("foreign-scoped duplicate of an expected identity was ignored, want extra-record failure")
			}
			if !strings.Contains(err.Error(), "extra") {
				t.Fatalf("error = %q, want extra-record diagnostic", err)
			}
		})
	}
}

func TestAssertRationaleMaterializedEdgeRecordsRejectsMispositionedExpectedIdentity(t *testing.T) {
	t.Parallel()
	expected := rationaleRecordFixture()
	base := rationaleGraphEdges(expected)
	tests := map[string]func(graphdump.Edge) graphdump.Edge{
		"expected target used as source": func(edge graphdump.Edge) graphdump.Edge {
			edge.FromLabels = append([]string(nil), edge.ToLabels...)
			edge.FromProps = edge.ToProps
			edge.ToLabels = []string{"Function"}
			edge.ToProps = map[string]any{"uid": "content-entity:foreign", "repo_id": "repository:foreign"}
			return edge
		},
		"expected source used as target": func(edge graphdump.Edge) graphdump.Edge {
			edge.ToLabels = append([]string(nil), edge.FromLabels...)
			edge.ToProps = edge.FromProps
			edge.FromLabels = []string{"Rationale"}
			edge.FromProps = map[string]any{"uid": "rationale:foreign", "repo_id": "repository:foreign"}
			return edge
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			extra := mutate(cloneGraphEdge(t, base[0]))
			extra.FromProps["repo_id"] = "repository:foreign"
			extra.ToProps["repo_id"] = "repository:foreign"
			actual := append(rationaleGraphEdges(expected), extra)
			err := assertRationaleMaterializedEdgeRecords(
				context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, expected,
			)
			if err == nil {
				t.Fatal("mispositioned expected endpoint identity was ignored, want extra-record failure")
			}
			if !strings.Contains(err.Error(), "extra") {
				t.Fatalf("error = %q, want extra-record diagnostic", err)
			}
		})
	}
}

func TestAssertRationaleMaterializedEdgeRecordsMatchesJSONAndBoltIntegralNumbers(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(struct {
		Edges []ifa.RationaleExpectedEdgeRecord `json:"edges"`
	}{Edges: rationaleRecordFixture()})
	if err != nil {
		t.Fatalf("marshal expected records: %v", err)
	}
	var decoded struct {
		Edges []ifa.RationaleExpectedEdgeRecord `json:"edges"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal expected records: %v", err)
	}
	if _, ok := decoded.Edges[0].TargetRecord.Props["end_line"].(float64); !ok {
		t.Fatalf("JSON end_line type = %T, want float64", decoded.Edges[0].TargetRecord.Props["end_line"])
	}
	actual := rationaleGraphEdges(decoded.Edges)
	actual[0].ToProps["end_line"] = int64(2)
	if err := assertRationaleMaterializedEdgeRecords(
		context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, decoded.Edges,
	); err != nil {
		t.Fatalf("integral JSON float64 and live Bolt int64 compared unequal: %v", err)
	}
	actual[0].ToProps["end_line"] = int64(3)
	if err := assertRationaleMaterializedEdgeRecords(
		context.Background(), fakeEdgeReader{edges: actual}, rationaleRecordRepoID, decoded.Edges,
	); err == nil {
		t.Fatal("different integral JSON and Bolt values compared equal")
	}
}

func TestAssertRationaleMaterializedEdgeRecordsPropagatesReaderError(t *testing.T) {
	t.Parallel()
	want := errors.New("stream failed")
	err := assertRationaleMaterializedEdgeRecords(context.Background(), failingEdgeReader{err: want}, rationaleRecordRepoID, rationaleRecordFixture())
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapping %v", err, want)
	}
}

type failingEdgeReader struct{ err error }

func (f failingEdgeReader) StreamNodes(context.Context, func(graphdump.Node) error) error { return nil }
func (f failingEdgeReader) StreamEdges(context.Context, func(graphdump.Edge) error) error {
	return f.err
}

func rationaleRecordFixture() []ifa.RationaleExpectedEdgeRecord {
	return []ifa.RationaleExpectedEdgeRecord{
		{
			RelationshipType: "EXPLAINS",
			SourceEntityID:   "rationale:one",
			RationaleUID:     "rationale:one",
			TargetEntityID:   "content-entity:one",
			TargetPath:       "src/one.py",
			RepoID:           rationaleRecordRepoID,
			CommentKind:      "WHY",
			SourceRecord: ifa.RationaleExpectedNodeRecord{
				Labels: []string{"Evidence", "Rationale"},
				Props:  map[string]any{"uid": "rationale:one", "type": "rationale", "repo_id": rationaleRecordRepoID, "comment_kind": "WHY", "excerpt_hash": "one", "evidence_source": "reducer/rationale"},
			},
			EdgeProps: map[string]any{"confidence": 0.95, "reason": "Intent comment explains the code entity it precedes", "evidence_source": "reducer/rationale", "comment_kind": "WHY"},
			TargetRecord: ifa.RationaleExpectedNodeRecord{
				Labels: []string{"Code", "Function"},
				Props:  map[string]any{"uid": "content-entity:one", "id": "content-entity:one", "repo_id": rationaleRecordRepoID, "relative_path": "src/one.py", "language": "python", "end_line": 2},
			},
		},
	}
}

func rationaleGraphEdges(records []ifa.RationaleExpectedEdgeRecord) []graphdump.Edge {
	edges := make([]graphdump.Edge, 0, len(records))
	for _, record := range records {
		edges = append(edges, graphdump.Edge{
			Type:       record.RelationshipType,
			FromLabels: append([]string(nil), record.SourceRecord.Labels...),
			FromProps:  record.SourceRecord.Props,
			ToLabels:   append([]string(nil), record.TargetRecord.Labels...),
			ToProps:    record.TargetRecord.Props,
			Props:      record.EdgeProps,
		})
	}
	return cloneGraphEdgesForTest(edges)
}

func cloneGraphEdges(t *testing.T, edges []graphdump.Edge) []graphdump.Edge {
	t.Helper()
	records := make([]ifa.RationaleExpectedEdgeRecord, 0, len(edges))
	for _, edge := range edges {
		records = append(records, ifa.RationaleExpectedEdgeRecord{
			RelationshipType: edge.Type,
			SourceRecord:     ifa.RationaleExpectedNodeRecord{Labels: edge.FromLabels, Props: edge.FromProps},
			EdgeProps:        edge.Props,
			TargetRecord:     ifa.RationaleExpectedNodeRecord{Labels: edge.ToLabels, Props: edge.ToProps},
		})
	}
	return rationaleGraphEdges(records)
}

func cloneGraphEdge(t *testing.T, edge graphdump.Edge) graphdump.Edge {
	t.Helper()
	return cloneGraphEdges(t, []graphdump.Edge{edge})[0]
}

func cloneGraphEdgesForTest(edges []graphdump.Edge) []graphdump.Edge {
	out := make([]graphdump.Edge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, graphdump.Edge{
			Type:       edge.Type,
			FromLabels: append([]string(nil), edge.FromLabels...),
			FromProps:  cloneProps(edge.FromProps),
			ToLabels:   append([]string(nil), edge.ToLabels...),
			ToProps:    cloneProps(edge.ToProps),
			Props:      cloneProps(edge.Props),
		})
	}
	return out
}

func cloneProps(props map[string]any) map[string]any {
	if props == nil {
		return nil
	}
	out := make(map[string]any, len(props))
	for key, value := range props {
		out[key] = value
	}
	return out
}
