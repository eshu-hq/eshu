// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
)

// iamCanAssumeFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const iamCanAssumeFamily = "iam_can_assume"

// iamCanAssumeRelationshipType is the single relationship type
// canonicalIAMCanAssumeEdgeUpsertCypherFormat MERGEs, after the closed
// iamCanAssumeRelationshipVocabulary token is substituted for its %s.
//
// It is NOT iamCanAssumeEdgeLabel ("IAM_CAN_ASSUME"), which the writer
// attaches as statement metadata beside the query and which never reaches
// the graph. Taking the type from that label is the same name-derived
// mistake at one level below the port name, and it would make this guard
// assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const iamCanAssumeRelationshipType = "CAN_ASSUME"

// iamCanAssumeExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const iamCanAssumeExpectedEdgesRelPath = "go/internal/ifa/testdata/iamcanassume/ifa-iam-can-assume-family-expected-edges.json"

// iamCanAssumeExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func iamCanAssumeExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanAssumeExpectedEdgesRelPath)
}

// resolveIAMCanAssumeMaterializedEdges is iam_can_assume's named vacuity
// guard (#6228).
//
// Like iam_instance_profile_role, this family's extractor already emits
// exactly the rows the write template UNWINDs, so the rows-to-edges mapping
// is one-for-one with no routing predicate to reproduce. What the fixture
// proves instead is the extractor's resolution behaviour: one Allow trust
// statement fans out one edge per DISTINCT resolved principal across both
// principal kinds, while a Deny, a wildcard, an AWS-service principal, an
// unscanned foreign ARN, an unscanned source role, a self-assume, and a
// non-trust source each resolve to nothing rather than to a fabricated edge.
//
// The Odù's facts are partitioned with the production
// iamcan.SplitIAMCanAssumeEnvelopes the handler itself calls, not a guard-side
// copy of it, so a partition regression reds the handler and this guard
// together instead of letting the two disagree.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// aws_iam_permission contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveIAMCanAssumeMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, iamCanAssumeFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	resources, permissions := iamcan.SplitIAMCanAssumeEnvelopes(odu.Facts)
	rows, _, quarantined, err := iamcan.ExtractIAMCanAssumeEdgeRows(resources, permissions)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractIAMCanAssumeEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_iam_permission contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractIAMCanAssumeEdgeRows produced zero CAN_ASSUME rows; this fixture cannot prove anything", odu.Name)
	}

	// The extractor stamps relationship_type onto each row from its own
	// edgetype.CanAssume constant, and the writer screens that value against
	// the closed vocabulary before interpolating it into the MERGE. Checking
	// it here binds the third copy -- this guard's literal -- to the other
	// two, so a family that started emitting a different token cannot pass by
	// having its fixture updated to match.
	for index, row := range rows {
		if got := anyToStringValue(row["relationship_type"]); got != iamCanAssumeRelationshipType {
			return false, fmt.Sprintf(
				"odù %q: extractor row %d carries relationship_type %q, want %q; the writer screens this value against a closed single-member vocabulary, so a different token would be rejected at write time rather than materialized",
				odu.Name, index, got, iamCanAssumeRelationshipType,
			)
		}
	}

	actual := iamCanAssumeRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, iamCanAssumeFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractIAMCanAssumeEdgeRows reproduces the expected %d-edge CAN_ASSUME set exactly across all %d registry type(s) and %d fact envelope(s); the deny, wildcard, service-principal, foreign, ghost-source, self-assume, and inline-source statements produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// iamCanAssumeRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// principal_uid and role_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. principal_kind and resolution_mode are SET after the MERGE --
// deliberately outside the relationship key, so a re-resolution through a
// different kind or mode converges on one edge rather than duplicating it --
// and are asserted here as mutable properties so a stale mode cannot pass on
// edge identity alone.
//
// scope_id, generation_id and evidence_source are stamped by the WRITER from
// its own per-run intent arguments (and its reducer-owned evidence-source
// constant) rather than carried on the extractor's rows, so they are not part
// of what this offline guard can or should assert; no static set can pin
// per-run values. Their stamping is covered by the writer unit test asserting
// the annotated row contents instead.
func iamCanAssumeRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["principal_uid"]),
			TargetEntityID:   anyToStringValue(row["role_uid"]),
			Properties: map[string]string{
				"principal_kind":  anyToStringValue(row["principal_kind"]),
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
