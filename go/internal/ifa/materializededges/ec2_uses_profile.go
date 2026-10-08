// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2usesprofile"
)

// ec2UsesProfileFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const ec2UsesProfileFamily = "ec2_uses_profile"

// ec2UsesProfileRelationshipType is the single relationship type the
// canonical EC2 uses-profile upsert template MERGEs: the one member of the
// writer's closed vocabulary substituted into the template's one %s.
//
// Read it off the template and the ec2UsesProfileRelationshipType const,
// never by deriving from the port or family name — "EC2_USES_PROFILE"
// appears nowhere in code: taking the type from the port or family name is
// the same name-derived mistake one level above, and it would make this
// guard assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const ec2UsesProfileRelationshipType = "USES_PROFILE"

// ec2UsesProfileResolutionMode is the writer's only resolution mode,
// stamped on every row. The extractor keys the source on the instance id
// where the posture carries one and on the full ARN where it does not,
// but both paths converge on this single mode: pinning it catches a
// future second mode silently changing what the rows claim.
const ec2UsesProfileResolutionMode = "arn"

// ec2UsesProfileExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const ec2UsesProfileExpectedEdgesRelPath = "go/internal/ifa/testdata/ec2usesprofile/ifa-ec2-uses-profile-family-expected-edges.json"

// ec2UsesProfileExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func ec2UsesProfileExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, ec2UsesProfileExpectedEdgesRelPath)
}

// resolveEC2UsesProfileMaterializedEdges is ec2_uses_profile's named vacuity
// guard (#6228).
//
// Like iam_escalation, this family's extractor already emits exactly the rows
// the write template UNWINDs, so the rows-to-edges mapping is one-for-one
// with no routing predicate to reproduce. What the fixture proves instead is
// the extractor's resolution behaviour: two instances resolve by id against
// scanned profiles while a third resolves by ARN fallback with a blank
// instance id — while a bare instance with no profile, a terminated
// instance, and an instance naming an unscanned profile each resolve to
// nothing rather than to a fabricated edge. The resolution mode is pinned
// on every row (single-mode contract), and the skip taxonomy itself stays
// covered by the reducer's own unit tests: the guard asserts the exact
// three-edge set, so a ghost edge or a dropped producer reds here either
// way.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own split uses, not a guard-side copy of richer logic: the
// production split is a pure kind partition (resources vs postures,
// everything else dropped), so reproducing its two cases here keeps a
// partition regression red in both places instead of letting the two
// disagree. Tombstoned envelopes pass through to the extractor on both
// sides alike: the tombstone filter lives inside the extraction, not in the
// split.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// ec2_instance_posture contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveEC2UsesProfileMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, ec2UsesProfileFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, postures []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case cloud.EC2InstancePostureFactKind:
			postures = append(postures, env)
		}
	}
	rows, _, quarantined, err := ec2usesprofile.ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractEC2UsesProfileEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / ec2_instance_posture contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractEC2UsesProfileEdgeRows produced zero USES_PROFILE rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["resolution_mode"]); got != ec2UsesProfileResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, ec2UsesProfileResolutionMode)
		}
	}

	actual := ec2UsesProfileRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, ec2UsesProfileFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractEC2UsesProfileEdgeRows reproduces the expected %d-edge USES_PROFILE set exactly across all %d registry type(s) and %d fact envelope(s); the bare, terminated, and ghost-profile instances produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// ec2UsesProfileRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// source_uid and target_uid are both :CloudResource uids, matched by the
// template's two MATCH clauses, so they are the edge's source and target
// identity. resolution_mode rides along as a Properties pin: the template
// SETs it as a plain string from the row (unlike scope_id/generation_id/
// evidence_source, which the WRITER stamps from its own per-run intent
// arguments rather than carrying on the extractor's rows — no static set
// can pin per-run values, and their stamping is covered by the writer unit
// test asserting the annotated row contents instead). The live assert reads
// only fixture-declared property keys off each rel, so extra live props
// never collide with this pin.
func ec2UsesProfileRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: ec2UsesProfileRelationshipType,
			SourceEntityID:   anyToStringValue(row["source_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
			Properties: map[string]string{
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
