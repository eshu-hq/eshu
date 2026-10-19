// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// cloudResourcePredicateEdge builds one fake graph edge for the predicate
// scan tests: a prefixed type between two :CloudResource endpoints carrying
// an evidence_source stamp.
func cloudResourcePredicateEdge(edgeType, fromUID, toUID, evidenceSource string) graphdump.Edge {
	return graphdump.Edge{
		Type:       edgeType,
		FromLabels: []string{"CloudResource"},
		ToLabels:   []string{"CloudResource"},
		FromProps:  map[string]any{"uid": fromUID},
		ToProps:    map[string]any{"uid": toUID},
		Props:      map[string]any{"evidence_source": evidenceSource},
	}
}

// TestMaterializedEdgeDomainPredicateResolvesCloudResource pins the
// materializededges half of the route-2 scan (#8358): cloud_resource has no
// finite type set, so MaterializedEdgeDomainEdgeTypes must keep failing
// closed for it while the predicate resolves.
func TestMaterializedEdgeDomainPredicateResolvesCloudResource(t *testing.T) {
	t.Parallel()

	if _, err := materializededges.MaterializedEdgeDomainEdgeTypes("cloud_resource"); err == nil {
		t.Fatal("MaterializedEdgeDomainEdgeTypes(cloud_resource) = nil error; the open vocabulary must not resolve as a finite type set")
	}
	predicate, ok := materializededges.MaterializedEdgeDomainPredicate("cloud_resource")
	if !ok {
		t.Fatal("MaterializedEdgeDomainPredicate(cloud_resource) did not resolve")
	}
	if _, ok := materializededges.MaterializedEdgeDomainPredicate("sql_relationships"); ok {
		t.Fatal("a finite-set family resolved as predicate-scoped")
	}
	// Pairing is same-writer: the AWS prefix with the GCP stamp is foreign.
	if predicate.Matches("AWS_USES_KMS_KEY", reducer.GCPRelationshipEvidenceSource(), []string{"CloudResource"}, []string{"CloudResource"}) {
		t.Error("predicate matched a cross-writer (prefix, evidence_source) pairing")
	}
	if !predicate.Matches("AWS_USES_KMS_KEY", reducer.AWSRelationshipEvidenceSource(), []string{"CloudResource"}, []string{"CloudResource"}) {
		t.Error("predicate did not match its own writer's edge shape")
	}
}

// TestRunAssertEdgesCommandCloudResourcePredicate drives the real command
// over a fake graph: exactly the predicate-matching edges pass, and every
// near-miss shape — wrong pairing, foreign stamp, wrong endpoint label,
// unprefixed type — is ignored rather than counted as an extra, while a
// predicate-matching edge absent from the fixture fails as an extra.
func TestRunAssertEdgesCommandCloudResourcePredicate(t *testing.T) {
	// Not parallel: swaps the package-level reader seam.
	expectedJSON := `{
  "odu": "odu:ifa-cloud-resource-family",
  "note": "test fixture",
  "edges": [
    {"relationship_type": "AWS_USES_KMS_KEY", "source_entity_id": "aws-src", "target_entity_id": "aws-dst",
     "properties": {"evidence_source": "` + reducer.AWSRelationshipEvidenceSource() + `"}},
    {"relationship_type": "GCP_INSTANCE_TO_DISK", "source_entity_id": "gcp-src", "target_entity_id": "gcp-dst",
     "properties": {"evidence_source": "` + reducer.GCPRelationshipEvidenceSource() + `"}},
    {"relationship_type": "AZURE_managed_by", "source_entity_id": "az-src", "target_entity_id": "az-dst",
     "properties": {"evidence_source": "` + reducer.AzureRelationshipEvidenceSource() + `"}}
  ]
}`
	expectedPath := filepath.Join(t.TempDir(), "expected.json")
	if err := os.WriteFile(expectedPath, []byte(expectedJSON), 0o600); err != nil {
		t.Fatalf("write expected fixture: %v", err)
	}

	graph := fakeEdgeReader{edges: []graphdump.Edge{
		cloudResourcePredicateEdge("AWS_USES_KMS_KEY", "aws-src", "aws-dst", reducer.AWSRelationshipEvidenceSource()),
		cloudResourcePredicateEdge("GCP_INSTANCE_TO_DISK", "gcp-src", "gcp-dst", reducer.GCPRelationshipEvidenceSource()),
		cloudResourcePredicateEdge("AZURE_managed_by", "az-src", "az-dst", reducer.AzureRelationshipEvidenceSource()),
		// Near misses the predicate must ignore:
		cloudResourcePredicateEdge("AWS_USES_KMS_KEY", "x1", "x2", reducer.GCPRelationshipEvidenceSource()), // cross pairing
		cloudResourcePredicateEdge("AWS_COVERS_VPC", "x3", "x4", "reducer/aws-observability-coverage"),      // prefixed, foreign stamp
		cloudResourcePredicateEdge("USES_KMS_KEY", "x5", "x6", reducer.AWSRelationshipEvidenceSource()),     // unprefixed
		{ // right type and stamp, wrong target label
			Type:       "AWS_USES_KMS_KEY",
			FromLabels: []string{"CloudResource"},
			ToLabels:   []string{"Workload"},
			FromProps:  map[string]any{"uid": "x7"},
			ToProps:    map[string]any{"uid": "x8"},
			Props:      map[string]any{"evidence_source": reducer.AWSRelationshipEvidenceSource()},
		},
		sqlEdge("CONTAINS", "r", "f"), // unrelated family noise
	}}

	original := openAssertEdgesReader
	openAssertEdgesReader = func(context.Context) (graphdump.Reader, func(), error) {
		return graph, func() {}, nil
	}
	t.Cleanup(func() { openAssertEdgesReader = original })

	var stdout, stderr bytes.Buffer
	err := runAssertEdgesCommand(context.Background(), []string{"-domain", "cloud_resource", "-expected", expectedPath}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runAssertEdgesCommand(cloud_resource exact predicate set) = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "expected=3") {
		t.Errorf("stdout = %q, want the 3-edge exact-match line", stdout.String())
	}

	// A predicate-matching edge the fixture does not name is an extra: the
	// open vocabulary must not make the gate blind to spurious verbs.
	graph.edges = append(graph.edges,
		cloudResourcePredicateEdge("AWS_BRAND_NEW_VERB", "aws-src", "aws-dst", reducer.AWSRelationshipEvidenceSource()))
	err = runAssertEdgesCommand(context.Background(), []string{"-domain", "cloud_resource", "-expected", expectedPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "AWS_BRAND_NEW_VERB") {
		t.Fatalf("runAssertEdgesCommand(unexpected predicate edge) = %v, want an extra-edge failure naming the verb", err)
	}
}

// Fixture scope ids for the #8358 cloud-resource cassette, read off
// testdata/cassettes/cloudresource/ifa-cloud-resource-family.json: one
// generation per provider writer. The ambient scope stands in for the
// demo-org + synth-multiscope GCP edges sharing the live cell graph.
const (
	cloudResourceTestAWSScope   = "aws:eshu-fixture-cloud-resource-account"
	cloudResourceTestGCPScope   = "gcp:project:eshu-fixture-project"
	cloudResourceTestAzureScope = "azure:eshu-fixture-tenant:subscription:eshu-fixture-rg"
	cloudResourceTestOtherScope = "gcp:project:ambient-other"
)

// cloudResourceScopedEdge is cloudResourcePredicateEdge plus the writer's
// rel.scope_id stamp, the field the -scopes filter keys on.
func cloudResourceScopedEdge(edgeType, fromUID, toUID, evidenceSource, scopeID string) graphdump.Edge {
	edge := cloudResourcePredicateEdge(edgeType, fromUID, toUID, evidenceSource)
	if edge.Props == nil {
		edge.Props = map[string]any{}
	}
	edge.Props["scope_id"] = scopeID
	return edge
}

// cloudResourceScopedExpected is the 4-edge fixture shape with evidence_source
// asserted (scope_id stays per-run intent, never pinned by the fixture).
func cloudResourceScopedExpected() []materializededges.ExpectedEdge {
	return []materializededges.ExpectedEdge{
		{
			RelationshipType: "AWS_USES_KMS_KEY", SourceEntityID: "aws-src", TargetEntityID: "aws-dst",
			Properties: map[string]string{"evidence_source": reducer.AWSRelationshipEvidenceSource()},
		},
		{
			RelationshipType: "AWS_USES_SUBNET", SourceEntityID: "aws-src", TargetEntityID: "aws-net",
			Properties: map[string]string{"evidence_source": reducer.AWSRelationshipEvidenceSource()},
		},
		{
			RelationshipType: "GCP_INSTANCE_TO_DISK", SourceEntityID: "gcp-src", TargetEntityID: "gcp-dst",
			Properties: map[string]string{"evidence_source": reducer.GCPRelationshipEvidenceSource()},
		},
		{
			RelationshipType: "AZURE_managed_by", SourceEntityID: "az-src", TargetEntityID: "az-dst",
			Properties: map[string]string{"evidence_source": reducer.AzureRelationshipEvidenceSource()},
		},
	}
}

// cloudResourceScopedGraph is the 4 fixture edges (in-scope stamps) plus one
// ambient out-of-scope GCP_synthetic_contained_in edge carrying the same GCP
// stamp and :CloudResource endpoints as the fixture's own GCP edge.
func cloudResourceScopedGraph() []graphdump.Edge {
	return []graphdump.Edge{
		cloudResourceScopedEdge("AWS_USES_KMS_KEY", "aws-src", "aws-dst", reducer.AWSRelationshipEvidenceSource(), cloudResourceTestAWSScope),
		cloudResourceScopedEdge("AWS_USES_SUBNET", "aws-src", "aws-net", reducer.AWSRelationshipEvidenceSource(), cloudResourceTestAWSScope),
		cloudResourceScopedEdge("GCP_INSTANCE_TO_DISK", "gcp-src", "gcp-dst", reducer.GCPRelationshipEvidenceSource(), cloudResourceTestGCPScope),
		cloudResourceScopedEdge("AZURE_managed_by", "az-src", "az-dst", reducer.AzureRelationshipEvidenceSource(), cloudResourceTestAzureScope),
		cloudResourceScopedEdge("GCP_synthetic_contained_in", "gcp-src", "gcp-dst", reducer.GCPRelationshipEvidenceSource(), cloudResourceTestOtherScope),
	}
}

// TestAssertMaterializedEdgesScopedIgnoresOutOfScopePredicateEdges is the
// #8358 RED/GREEN: the shared cell graph holds 627 ambient GCP edges with
// the same (prefix, stamp, endpoints) shape, so the unscoped predicate scan
// reports the ambient edge as an extra, while the cassette-scoped scan
// ignores it.
func TestAssertMaterializedEdgesScopedIgnoresOutOfScopePredicateEdges(t *testing.T) {
	predicate, ok := materializededges.MaterializedEdgeDomainPredicate("cloud_resource")
	if !ok {
		t.Fatal("MaterializedEdgeDomainPredicate(cloud_resource) did not resolve")
	}
	expected := cloudResourceScopedExpected()
	graph := cloudResourceScopedGraph()

	// Unscoped preserves current behavior: the ambient edge is an extra.
	err := assertMaterializedEdgesScoped(context.Background(), fakeEdgeReader{edges: graph},
		"cloud_resource", nil, &predicate, nil, nil, expected, nil)
	if err == nil || !strings.Contains(err.Error(), "GCP_synthetic_contained_in") {
		t.Fatalf("unscoped assertMaterializedEdgesScoped(4 fixture + 1 ambient) = %v, want an extra-edge failure naming the ambient verb", err)
	}

	// Scoped to the cassette's three scope ids: the ambient edge drops out.
	scopes := map[string]struct{}{
		cloudResourceTestAWSScope:   {},
		cloudResourceTestGCPScope:   {},
		cloudResourceTestAzureScope: {},
	}
	if err := assertMaterializedEdgesScoped(context.Background(), fakeEdgeReader{edges: graph},
		"cloud_resource", nil, &predicate, nil, nil, expected, scopes); err != nil {
		t.Fatalf("scoped assertMaterializedEdgesScoped(4 fixture + 1 ambient) = %v, want nil", err)
	}
}

// TestAssertMaterializedEdgesScopedStillCatchesInScopeViolation is the
// seeded-violation pair: scoping must not blind the gate to a real
// in-scope spurious edge, and the clean tree still passes.
func TestAssertMaterializedEdgesScopedStillCatchesInScopeViolation(t *testing.T) {
	predicate, ok := materializededges.MaterializedEdgeDomainPredicate("cloud_resource")
	if !ok {
		t.Fatal("MaterializedEdgeDomainPredicate(cloud_resource) did not resolve")
	}
	expected := cloudResourceScopedExpected()
	scopes := map[string]struct{}{
		cloudResourceTestAWSScope:   {},
		cloudResourceTestGCPScope:   {},
		cloudResourceTestAzureScope: {},
	}

	clean := cloudResourceScopedGraph()[:4]
	if err := assertMaterializedEdgesScoped(context.Background(), fakeEdgeReader{edges: clean},
		"cloud_resource", nil, &predicate, nil, nil, expected, scopes); err != nil {
		t.Fatalf("scoped assertMaterializedEdgesScoped(clean 4-edge tree) = %v, want nil", err)
	}

	planted := append(append([]graphdump.Edge(nil), clean...),
		cloudResourceScopedEdge("AWS_BRAND_NEW_VERB", "aws-src", "aws-dst", reducer.AWSRelationshipEvidenceSource(), cloudResourceTestAWSScope))
	err := assertMaterializedEdgesScoped(context.Background(), fakeEdgeReader{edges: planted},
		"cloud_resource", nil, &predicate, nil, nil, expected, scopes)
	if err == nil || !strings.Contains(err.Error(), "AWS_BRAND_NEW_VERB") {
		t.Fatalf("scoped assertMaterializedEdgesScoped(planted in-scope violation) = %v, want an extra-edge failure naming the verb", err)
	}
}

// TestRunAssertEdgesCommandCloudResourceScopesFlag threads -scopes from argv
// to the scan: the same 4+1 graph fails without the flag and passes with the
// cassette's three scope ids.
func TestRunAssertEdgesCommandCloudResourceScopesFlag(t *testing.T) {
	expectedJSON := `{
  "odu": "odu:ifa-cloud-resource-family",
  "note": "test fixture",
  "edges": [
    {"relationship_type": "AWS_USES_KMS_KEY", "source_entity_id": "aws-src", "target_entity_id": "aws-dst",
     "properties": {"evidence_source": "` + reducer.AWSRelationshipEvidenceSource() + `"}},
    {"relationship_type": "AWS_USES_SUBNET", "source_entity_id": "aws-src", "target_entity_id": "aws-net",
     "properties": {"evidence_source": "` + reducer.AWSRelationshipEvidenceSource() + `"}},
    {"relationship_type": "GCP_INSTANCE_TO_DISK", "source_entity_id": "gcp-src", "target_entity_id": "gcp-dst",
     "properties": {"evidence_source": "` + reducer.GCPRelationshipEvidenceSource() + `"}},
    {"relationship_type": "AZURE_managed_by", "source_entity_id": "az-src", "target_entity_id": "az-dst",
     "properties": {"evidence_source": "` + reducer.AzureRelationshipEvidenceSource() + `"}}
  ]
}`
	expectedPath := filepath.Join(t.TempDir(), "expected.json")
	if err := os.WriteFile(expectedPath, []byte(expectedJSON), 0o600); err != nil {
		t.Fatalf("write expected fixture: %v", err)
	}
	graph := fakeEdgeReader{edges: cloudResourceScopedGraph()}
	original := openAssertEdgesReader
	openAssertEdgesReader = func(context.Context) (graphdump.Reader, func(), error) {
		return graph, func() {}, nil
	}
	t.Cleanup(func() { openAssertEdgesReader = original })

	var stdout, stderr bytes.Buffer
	err := runAssertEdgesCommand(context.Background(),
		[]string{"-domain", "cloud_resource", "-expected", expectedPath}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "GCP_synthetic_contained_in") {
		t.Fatalf("runAssertEdgesCommand(unscoped 4+1) = %v, want the ambient extra", err)
	}
	scopes := cloudResourceTestAWSScope + "," + cloudResourceTestGCPScope + "," + cloudResourceTestAzureScope
	if err := runAssertEdgesCommand(context.Background(),
		[]string{"-domain", "cloud_resource", "-expected", expectedPath, "-scopes", scopes}, &stdout, &stderr); err != nil {
		t.Fatalf("runAssertEdgesCommand(scoped 4+1) = %v, want nil", err)
	}
}
