// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// workloadCloudRelationshipFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const workloadCloudRelationshipFamily = "workload_cloud_relationship"

// workloadCloudRelationshipRelationshipType is the single relationship type
// workloadCloudRelationshipUpsertCypherFormat MERGEs, after the closed
// workloadCloudRelationshipVocabulary token is substituted for its %s.
//
// It is NOT workloadCloudRelationshipEdgeLabel ("WORKLOAD_USES_CLOUD_RESOURCE"),
// which the writer attaches as statement metadata beside the query and which
// never reaches the graph. Taking the type from that label is the same
// name-derived mistake at one level below the port name, and it would make this
// guard assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const workloadCloudRelationshipRelationshipType = "USES"

// workloadCloudRelationshipExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const workloadCloudRelationshipExpectedEdgesRelPath = "go/internal/ifa/testdata/workloadcloudrelationship/ifa-workload-cloud-relationship-family-expected-edges.json"

// workloadCloudRelationshipExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func workloadCloudRelationshipExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, workloadCloudRelationshipExpectedEdgesRelPath)
}

// resolveWorkloadCloudRelationshipMaterializedEdges is
// workload_cloud_relationship's named vacuity guard (#6228).
//
// Like iam_instance_profile_role, this family's extractor already emits
// exactly the rows the write template UNWINDs, so the rows-to-edges mapping is
// one-for-one with no routing predicate to reproduce. What the fixture proves
// instead is the extractor's anchor behaviour: exactly one explicit workload
// anchor promotes a resource to an edge, while a service-only anchor, an
// ambiguous (multi-workload) anchor, and an anchor with no environment each
// resolve to nothing rather than to a fabricated edge.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource
// contract cannot prove the set it names, and the surviving facts would
// understate its own claim.
func resolveWorkloadCloudRelationshipMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, workloadCloudRelationshipFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, _, quarantined, err := reducer.ExtractWorkloadCloudRelationshipRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractWorkloadCloudRelationshipRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractWorkloadCloudRelationshipRows produced zero USES rows; this fixture cannot prove anything", odu.Name)
	}

	// The extractor stamps relationship_type onto each row from its own
	// edgetype.Uses constant, and the writer screens that value against the
	// closed vocabulary before interpolating it into the MERGE. Checking it
	// here binds the third copy -- this guard's literal -- to the other two, so
	// a family that started emitting a different token cannot pass by having
	// its fixture updated to match.
	for index, row := range rows {
		if got := anyToStringValue(row["relationship_type"]); got != workloadCloudRelationshipRelationshipType {
			return false, fmt.Sprintf(
				"odù %q: extractor row %d carries relationship_type %q, want %q; the writer screens this value against a closed single-member vocabulary, so a different token would be rejected at write time rather than materialized",
				odu.Name, index, got, workloadCloudRelationshipRelationshipType,
			)
		}
	}

	actual := workloadCloudRelationshipRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, workloadCloudRelationshipFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractWorkloadCloudRelationshipRows reproduces the expected %d-edge USES set exactly across all %d registry type(s) and %d fact envelope(s); the service-only, ambiguous and environment-less anchors produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// workloadCloudRelationshipRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// workload_id is the edge's source identity: the template MATCHes
// `(workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance)` and
// MERGEs the edge off the instance, so the workload id passed through
// VERBATIM is what the edge anchors on. environment disambiguates the
// instance (the extractor dedupes on workload+environment+resource and the
// template filters `instance.environment = row.environment`), so it is
// asserted as a property even though the relationship MERGEs on its endpoints
// alone. cloud_resource_uid is the :CloudResource target uid.
//
// resolution_mode, relationship_basis, service_anchor_source and
// service_anchor_reason are SET after the MERGE -- deliberately outside the
// relationship key -- and are asserted here as mutable properties so a stale
// anchor decision cannot pass on edge identity alone.
//
// source_fact_id, stable_fact_key, source_system, source_record_id and
// collector_kind are stamped from the fact envelope's transport identity,
// which a compiled Odù does not carry (the cassette projection drops them for
// the same reason); they belong to the live `assert-edges` half of the
// family's proof, not to this offline comparison. scope_id, generation_id and
// evidence_source are stamped by the WRITER from its own arguments rather
// than carried on the extractor's rows, so they are outside what this guard
// can or should assert for the same reason iam_instance_profile_role's guard
// does not assert them.
func workloadCloudRelationshipRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["workload_id"]),
			TargetEntityID:   anyToStringValue(row["cloud_resource_uid"]),
			Properties: map[string]string{
				"resolution_mode":       anyToStringValue(row["resolution_mode"]),
				"environment":           anyToStringValue(row["environment"]),
				"relationship_basis":    anyToStringValue(row["relationship_basis"]),
				"service_anchor_source": anyToStringValue(row["service_anchor_source"]),
				"service_anchor_reason": anyToStringValue(row["service_anchor_reason"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
