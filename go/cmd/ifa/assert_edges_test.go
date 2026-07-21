// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
)

// fakeEdgeReader is an in-memory graphdump.Reader for the hermetic
// assert-edges set-comparison tests: no Bolt, no Docker.
type fakeEdgeReader struct {
	edges []graphdump.Edge
}

func (f fakeEdgeReader) StreamNodes(_ context.Context, _ func(graphdump.Node) error) error {
	return nil
}

func (f fakeEdgeReader) StreamEdges(_ context.Context, yield func(graphdump.Edge) error) error {
	for _, e := range f.edges {
		if err := yield(e); err != nil {
			return err
		}
	}
	return nil
}

func sqlEdge(edgeType, fromUID, toUID string) graphdump.Edge {
	return graphdump.Edge{
		Type:      edgeType,
		FromProps: map[string]any{"uid": fromUID},
		ToProps:   map[string]any{"uid": toUID},
	}
}

func sqlEdgeTypesForTest(t *testing.T) map[string]struct{} {
	t.Helper()
	set, err := ifa.MaterializedEdgeDomainEdgeTypes("sql_relationships")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(sql_relationships): %v", err)
	}
	return set
}

// TestParseAssertEdgesFlagsRequiresDomainAndExpected proves both required
// flags are enforced before any backend connection.
func TestParseAssertEdgesFlagsRequiresDomainAndExpected(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	if _, err := parseAssertEdgesFlags([]string{"-expected", "x.json"}, &stderr); err == nil {
		t.Error("parseAssertEdgesFlags without -domain = nil error, want required-flag error")
	}
	if _, err := parseAssertEdgesFlags([]string{"-domain", "sql_relationships"}, &stderr); err == nil {
		t.Error("parseAssertEdgesFlags without -expected = nil error, want required-flag error")
	}
	o, err := parseAssertEdgesFlags([]string{"-domain", "sql_relationships", "-expected", "x.json"}, &stderr)
	if err != nil {
		t.Fatalf("parseAssertEdgesFlags(valid): %v", err)
	}
	if o.domain != "sql_relationships" || o.expected != "x.json" {
		t.Errorf("parsed options = %+v, want domain/expected plumbed through", o)
	}
}

// TestRunAssertEdgesCommandRejectsUnknownDomainWithoutBackend proves an
// unregistered family fails fast (before any Bolt dial) with a clear message,
// hermetically testable in CI with no graph backend.
func TestRunAssertEdgesCommandRejectsUnknownDomainWithoutBackend(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := runAssertEdgesCommand(context.Background(), []string{"-domain", "bogus_family", "-expected", "x.json"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runAssertEdgesCommand(bogus domain) = nil error, want an unknown-family error")
	}
	if !strings.Contains(err.Error(), "bogus_family") {
		t.Errorf("error %q does not name the unknown family", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want none on a pre-backend error", stdout.String())
	}
}

// TestAssertMaterializedEdgesExactMatch is the honest-green case: a fake graph
// carrying exactly the 7 expected SQL edges (plus unrelated CONTAINS edges the
// filter must ignore) passes.
func TestAssertMaterializedEdgesExactMatch(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
		{RelationshipType: "READS_FROM", SourceEntityID: "v", TargetEntityID: "t"},
		{RelationshipType: "TRIGGERS", SourceEntityID: "trg", TargetEntityID: "t"},
		{RelationshipType: "EXECUTES", SourceEntityID: "trg", TargetEntityID: "fn"},
		{RelationshipType: "INDEXES", SourceEntityID: "idx", TargetEntityID: "t"},
		{RelationshipType: "MIGRATES", SourceEntityID: "mig", TargetEntityID: "t"},
		{RelationshipType: "QUERIES_TABLE", SourceEntityID: "gofn", TargetEntityID: "t"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
		sqlEdge("READS_FROM", "v", "t"),
		sqlEdge("TRIGGERS", "trg", "t"),
		sqlEdge("EXECUTES", "trg", "fn"),
		sqlEdge("INDEXES", "idx", "t"),
		sqlEdge("MIGRATES", "mig", "t"),
		sqlEdge("QUERIES_TABLE", "gofn", "t"),
		// Unrelated edges the family filter must ignore.
		sqlEdge("CONTAINS", "f", "t"),
		sqlEdge("REPO_CONTAINS", "r", "f"),
	}}

	if err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected); err != nil {
		t.Fatalf("assertMaterializedEdges(exact match) = %v, want nil", err)
	}
}

// TestAssertMaterializedEdgesMissingEdgeFails is the vacuity-catching case: a
// graph missing one expected edge (e.g. a family that silently stopped
// materializing MIGRATES) fails, naming the missing edge.
func TestAssertMaterializedEdgesMissingEdgeFails(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
		{RelationshipType: "MIGRATES", SourceEntityID: "mig", TargetEntityID: "t"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(missing MIGRATES) = nil, want a missing-edge failure")
	}
	if !strings.Contains(err.Error(), "MIGRATES|mig|t") {
		t.Errorf("error %q does not name the missing MIGRATES edge", err)
	}
}

// TestAssertMaterializedEdgesEmptyGraphFailsNotVacuous is the exact regression
// the P2 digest can't catch: a family that materialized ZERO edges in the
// graph must FAIL, not pass — the whole reason this live assertion exists
// alongside digest equality.
func TestAssertMaterializedEdgesEmptyGraphFailsNotVacuous(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		// Only unrelated edges — the SQL family is entirely absent.
		sqlEdge("CONTAINS", "f", "t"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(empty family) = nil, want failure — a silently-empty family must not pass vacuously")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not report the missing family edges", err)
	}
}

// TestAssertMaterializedEdgesExtraEdgeFails proves a spurious family edge in
// the graph (fixture drift or an over-materialization) fails too.
func TestAssertMaterializedEdgesExtraEdgeFails(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
		sqlEdge("HAS_COLUMN", "t", "c2"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(extra edge) = nil, want an extra-edge failure")
	}
	if !strings.Contains(err.Error(), "extra") || !strings.Contains(err.Error(), "HAS_COLUMN|t|c2") {
		t.Errorf("error %q does not name the extra edge", err)
	}
}

// TestAssertMaterializedEdgesMissingEndpointUIDFails proves an edge whose
// endpoint node has no uid (an unmaterialized endpoint — the exact silent
// no-op #5351's fixture work surfaced) is reported as an endpoint defect, not
// silently skipped.
func TestAssertMaterializedEdgesMissingEndpointUIDFails(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		{Type: "HAS_COLUMN", FromProps: map[string]any{"uid": "t"}, ToProps: map[string]any{}},
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(no-uid endpoint) = nil, want an endpoint-defect failure")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error %q does not report the endpoint defect", err)
	}
}

// TestAssertMaterializedEdgesDuplicateEdgeFails proves a deterministic
// duplicate edge — the SAME (type, source_uid, target_uid) written twice, e.g.
// a concurrent MERGE race or a duplicate writer output — is a MISMATCH, not a
// silent collapse. The command promises an exact edge COUNT; keying the actual
// set by identity alone would let two identical edges collapse to one and pass
// both this assertion and the cross-worker digest, so multiplicity must be
// tracked. (P2 on #5549.)
func TestAssertMaterializedEdgesDuplicateEdgeFails(t *testing.T) {
	t.Parallel()

	expected := []ifa.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
		// The identical edge, materialized twice — a duplicate-edge regression.
		sqlEdge("HAS_COLUMN", "t", "c"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(duplicate edge) = nil, want a duplicate-edge failure — a duplicate must not silently collapse")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not report the duplicate edge", err)
	}
	if !strings.Contains(err.Error(), "HAS_COLUMN|t|c") {
		t.Errorf("error %q does not name the duplicated edge", err)
	}
}
