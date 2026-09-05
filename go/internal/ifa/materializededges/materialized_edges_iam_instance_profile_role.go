// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// iamInstanceProfileRoleFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const iamInstanceProfileRoleFamily = "iam_instance_profile_role"

// iamInstanceProfileRoleRelationshipType is the single relationship type
// canonicalIAMInstanceProfileRoleEdgeUpsertCypherFormat MERGEs, after the
// closed iamInstanceProfileRoleRelationshipVocabulary token is substituted for
// its %s.
//
// It is NOT iamInstanceProfileRoleEdgeLabel ("IAM_INSTANCE_PROFILE_HAS_ROLE"),
// which the writer attaches as statement metadata beside the query and which
// never reaches the graph. Taking the type from that label is the same
// name-derived mistake at one level below the port name, and it would make this
// guard assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const iamInstanceProfileRoleRelationshipType = "HAS_ROLE"

// iamInstanceProfileRoleExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const iamInstanceProfileRoleExpectedEdgesRelPath = "go/internal/ifa/testdata/iaminstanceprofilerole/ifa-iam-instance-profile-role-family-expected-edges.json"

// iamInstanceProfileRoleExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func iamInstanceProfileRoleExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamInstanceProfileRoleExpectedEdgesRelPath)
}

// resolveIAMInstanceProfileRoleMaterializedEdges is iam_instance_profile_role's
// named vacuity guard (#6228).
//
// Unlike kubernetes_namespace_environment, this family's extractor already
// emits exactly the rows the write template UNWINDs, so the rows-to-edges
// mapping is one-for-one with no routing predicate to reproduce. What the
// fixture proves instead is the extractor's resolution behaviour: an instance
// profile fans out one edge per DISTINCT resolved role, and a role ARN with no
// matching aws_iam_role fact in the same generation resolves to nothing rather
// than to a fabricated CloudResource.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource
// contract cannot prove the set it names, and the surviving facts would
// understate its own claim.
func resolveIAMInstanceProfileRoleMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, iamInstanceProfileRoleFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, _, quarantined, err := reducer.ExtractIAMInstanceProfileRoleEdgeRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractIAMInstanceProfileRoleEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractIAMInstanceProfileRoleEdgeRows produced zero HAS_ROLE rows; this fixture cannot prove anything", odu.Name)
	}

	// The extractor stamps relationship_type onto each row from its own
	// edgetype.HasRole constant, and the writer screens that value against the
	// closed vocabulary before interpolating it into the MERGE. Checking it
	// here binds the third copy -- this guard's literal -- to the other two, so
	// a family that started emitting a different token cannot pass by having
	// its fixture updated to match.
	for index, row := range rows {
		if got := anyToStringValue(row["relationship_type"]); got != iamInstanceProfileRoleRelationshipType {
			return false, fmt.Sprintf(
				"odù %q: extractor row %d carries relationship_type %q, want %q; the writer screens this value against a closed single-member vocabulary, so a different token would be rejected at write time rather than materialized",
				odu.Name, index, got, iamInstanceProfileRoleRelationshipType,
			)
		}
	}

	actual := iamInstanceProfileRoleRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, iamInstanceProfileRoleFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractIAMInstanceProfileRoleEdgeRows reproduces the expected %d-edge HAS_ROLE set exactly across all %d registry type(s) and %d fact envelope(s); the unresolved and unattached instance profiles produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// iamInstanceProfileRoleRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// profile_uid and role_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. resolution_mode is SET after the MERGE -- deliberately outside the
// relationship key, so a re-resolution through a different mode converges on
// one edge rather than duplicating it -- and is asserted here as a mutable
// property so a stale mode cannot pass on edge identity alone.
//
// scope_id and generation_id are stamped by the WRITER from its own per-run
// intent arguments rather than carried on the extractor's rows, so they are
// not part of what this offline guard can or should assert; no static set can
// pin per-run values. Their stamping is covered by the writer unit test
// asserting the annotated row contents instead.
//
// evidence_source is stamped by the writer too, but from a compile-time
// constant (reducer.IAMInstanceProfileRoleEvidenceSource), so it IS knowable
// offline and is stamped here: the live `assert-edges` half asserts it from
// the fixture, and both halves now prove the same key. A writer that stopped
// stamping it fails both, instead of passing the offline half on extractor
// identity while the live half stayed blind to it.
func iamInstanceProfileRoleRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["profile_uid"]),
			TargetEntityID:   anyToStringValue(row["role_uid"]),
			Properties: map[string]string{
				"evidence_source": reducer.IAMInstanceProfileRoleEvidenceSource,
			},
		}
		if mode := anyToStringValue(row["resolution_mode"]); mode != "" {
			edge.Properties["resolution_mode"] = mode
		}
		edges = append(edges, edge)
	}
	return edges
}
