// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
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
	set, err := materializededges.MaterializedEdgeDomainEdgeTypes("sql_relationships")
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

	expected := []materializededges.ExpectedEdge{
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

	if err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected); err != nil {
		t.Fatalf("assertMaterializedEdges(exact match) = %v, want nil", err)
	}
}

// TestAssertMaterializedEdgesMissingEdgeFails is the vacuity-catching case: a
// graph missing one expected edge (e.g. a family that silently stopped
// materializing MIGRATES) fails, naming the missing edge.
func TestAssertMaterializedEdgesMissingEdgeFails(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
		{RelationshipType: "MIGRATES", SourceEntityID: "mig", TargetEntityID: "t"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(missing MIGRATES) = nil, want a missing-edge failure")
	}
	wantLabel := expectedEdgeLabel(materializededges.ExpectedEdge{RelationshipType: "MIGRATES", SourceEntityID: "mig", TargetEntityID: "t"})
	if !strings.Contains(err.Error(), wantLabel) {
		t.Errorf("error %q does not name the missing MIGRATES edge (want label %q)", err, wantLabel)
	}
}

// TestAssertMaterializedEdgesEmptyGraphFailsNotVacuous is the exact regression
// the P2 digest can't catch: a family that materialized ZERO edges in the
// graph must FAIL, not pass — the whole reason this live assertion exists
// alongside digest equality.
func TestAssertMaterializedEdgesEmptyGraphFailsNotVacuous(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		// Only unrelated edges — the SQL family is entirely absent.
		sqlEdge("CONTAINS", "f", "t"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
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

	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
		sqlEdge("HAS_COLUMN", "t", "c2"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(extra edge) = nil, want an extra-edge failure")
	}
	wantLabel := expectedEdgeLabel(materializededges.ExpectedEdge{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c2"})
	if !strings.Contains(err.Error(), "extra") || !strings.Contains(err.Error(), wantLabel) {
		t.Errorf("error %q does not name the extra edge (want label %q)", err, wantLabel)
	}
}

// TestAssertMaterializedEdgesMissingEndpointIdentityFails proves an edge whose
// endpoint node carries NEITHER uid nor id (an unmaterialized endpoint — the
// exact silent no-op #5351's fixture work surfaced) is reported as an endpoint
// defect, not silently skipped.
//
// "Neither", not "no uid": endpoint identity is uid-first with an id fallback,
// because Repository, Workload, WorkloadInstance and Platform are id-keyed and
// carry no uid at all. An endpoint with only an id is legitimate and resolves.
func TestAssertMaterializedEdgesMissingEndpointIdentityFails(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		{Type: "HAS_COLUMN", FromProps: map[string]any{"uid": "t"}, ToProps: map[string]any{}},
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
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

	expected := []materializededges.ExpectedEdge{
		{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"},
	}
	reader := fakeEdgeReader{edges: []graphdump.Edge{
		sqlEdge("HAS_COLUMN", "t", "c"),
		// The identical edge, materialized twice — a duplicate-edge regression.
		sqlEdge("HAS_COLUMN", "t", "c"),
	}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(duplicate edge) = nil, want a duplicate-edge failure — a duplicate must not silently collapse")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not report the duplicate edge", err)
	}
	wantLabel := expectedEdgeLabel(materializededges.ExpectedEdge{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"})
	if !strings.Contains(err.Error(), wantLabel) {
		t.Errorf("error %q does not name the duplicated edge (want label %q)", err, wantLabel)
	}
}

// TestAssertMaterializedEdgesMissingDuplicateFails proves exact multiset
// comparison also rejects a graph with fewer copies than the fixture names.
// Checking only got==0 would let one graph edge satisfy two identical expected
// entries and contradict the command's exact-count contract.
func TestAssertMaterializedEdgesMissingDuplicateFails(t *testing.T) {
	t.Parallel()

	edge := materializededges.ExpectedEdge{RelationshipType: "HAS_COLUMN", SourceEntityID: "t", TargetEntityID: "c"}
	expected := []materializededges.ExpectedEdge{edge, edge}
	reader := fakeEdgeReader{edges: []graphdump.Edge{sqlEdge("HAS_COLUMN", "t", "c")}}

	err := assertMaterializedEdges(context.Background(), reader, "sql_relationships", sqlEdgeTypesForTest(t), nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(one of two expected copies) = nil, want a missing-multiplicity failure")
	}
	if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "graph=1, expected=2") {
		t.Fatalf("error %q does not report the missing multiplicity", err)
	}
}

// TestAssertMaterializedEdgesMissingAssertedPropertyFails is the lifecycle
// half of the direct-family proof (#6309): an edge whose endpoints and MERGE
// identity all match but that is missing one fixture-asserted property (e.g. a
// writer that stopped stamping evidence_source) must fail LOUDLY, not pass on
// identity alone. The two direct-family fixtures pin evidence_source exactly
// so this path is what holds their retraction predicates to the stamped
// value; without it a dropped stamp keeps every digest equal and the gate
// green while retraction silently stops finding prior edges.
func TestAssertMaterializedEdgesMissingAssertedPropertyFails(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "HAS_ROLE",
		SourceEntityID:   "profile-a",
		TargetEntityID:   "role-a",
		Properties: map[string]string{
			"resolution_mode": "arn",
			"evidence_source": "reducer/iam-instance-profile-role",
		},
	}}
	// Same endpoints, same resolution_mode, but no evidence_source: the exact
	// shape a writer that dropped the stamp would produce.
	reader := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:      "HAS_ROLE",
		FromProps: map[string]any{"uid": "profile-a"},
		ToProps:   map[string]any{"uid": "role-a"},
		Props:     map[string]any{"resolution_mode": "arn"},
	}}}

	types := map[string]struct{}{"HAS_ROLE": {}}
	err := assertMaterializedEdges(context.Background(), reader, "iam_instance_profile_role", types, nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(edge missing evidence_source) = nil, want an asserted-property failure")
	}
	if !strings.Contains(err.Error(), "asserted-property") || !strings.Contains(err.Error(), "evidence_source") {
		t.Fatalf("error %q does not name the dropped asserted property", err)
	}
}

// TestAssertMaterializedEdgesWrongAssertedPropertyValueFails closes the other
// half: a stamped-but-WRONG value (a writer stamping a different producer's
// source) must also fail rather than match on identity alone.
func TestAssertMaterializedEdgesWrongAssertedPropertyValueFails(t *testing.T) {
	t.Parallel()

	expected := []materializededges.ExpectedEdge{{
		RelationshipType: "HAS_ROLE",
		SourceEntityID:   "profile-a",
		TargetEntityID:   "role-a",
		Properties: map[string]string{
			"resolution_mode": "arn",
			"evidence_source": "reducer/iam-instance-profile-role",
		},
	}}
	reader := fakeEdgeReader{edges: []graphdump.Edge{{
		Type:      "HAS_ROLE",
		FromProps: map[string]any{"uid": "profile-a"},
		ToProps:   map[string]any{"uid": "role-a"},
		Props:     map[string]any{"resolution_mode": "arn", "evidence_source": "reducer/someone-else"},
	}}}

	types := map[string]struct{}{"HAS_ROLE": {}}
	err := assertMaterializedEdges(context.Background(), reader, "iam_instance_profile_role", types, nil, nil, expected)
	if err == nil {
		t.Fatal("assertMaterializedEdges(edge with wrong evidence_source) = nil, want a mismatch failure")
	}
}
