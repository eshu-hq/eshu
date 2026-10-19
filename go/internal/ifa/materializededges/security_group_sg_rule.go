// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
)

// securityGroupSGRuleFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.multiTypeDirectMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const securityGroupSGRuleFamily = "security_group_sg_rule"

// securityGroupSGRuleRelationshipTypes are the two relationship types the
// SG -> rule edge takes, one per rule direction, read off
// securityGroupAllowsRelTypeVocabulary (the closed set the writer screens
// each row's direction against) rather than derived from the port or family
// name.
//
// Duplicated here as plain literals rather than imported, and they fail
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const (
	securityGroupSGRuleIngressRelationshipType = "ALLOWS_INGRESS"
	securityGroupSGRuleEgressRelationshipType  = "ALLOWS_EGRESS"
)

// securityGroupSGRuleExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const securityGroupSGRuleExpectedEdgesRelPath = "go/internal/ifa/testdata/securitygroup/ifa-security-group-sg-rule-family-expected-edges.json"

// securityGroupSGRuleExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func securityGroupSGRuleExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, securityGroupSGRuleExpectedEdgesRelPath)
}

// resolveSecurityGroupSGRuleMaterializedEdges is security_group_sg_rule's
// named vacuity guard (#6228).
//
// Like iam_can_assume, this family's extractor already emits exactly the
// rows the write template UNWINDs, so the rows-to-edges mapping is
// one-for-one with no routing predicate to reproduce. What the fixture
// proves instead is the extractor's resolution behaviour across both
// directions of the closed vocabulary: ingress and egress rules resolve to
// their direction's type while an unscanned anchor, an unscanned referenced
// endpoint, an unknown source, and a tombstoned rule each resolve to nothing
// rather than to a fabricated edge.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own splitSecurityGroupReachabilityEnvelopes uses, not a
// guard-side copy of richer logic: the production split is a pure kind
// partition (resources vs rules, everything else dropped), so reproducing
// its two cases here keeps a partition regression red in both places
// instead of letting the two disagree.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the aws_resource or
// aws_security_group_rule contract cannot prove the set it names, and the
// surviving facts would understate its own claim.
func resolveSecurityGroupSGRuleMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, securityGroupSGRuleFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	var resources, rules []facts.Envelope
	for _, env := range odu.Facts {
		switch env.FactKind {
		case cloud.AWSResourceFactKind:
			resources = append(resources, env)
		case cloud.AWSSecurityGroupRuleFactKind:
			rules = append(rules, env)
		}
	}
	result, err := secgroup.ExtractSecurityGroupReachability(resources, rules)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractSecurityGroupReachability failed: %v", odu.Name, err)
	}
	if len(result.Quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the aws_resource / aws_security_group_rule contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(result.Quarantined))
	}
	if len(result.SGRuleEdges) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSecurityGroupReachability produced zero SG -> rule rows; this fixture cannot prove anything", odu.Name)
	}

	// The extractor stamps relationship_type onto each row from the closed
	// direction vocabulary, and the writer screens that value against its
	// own copy before interpolating it into the MERGE. Checking each row
	// against the guard's literals binds the third copy to the other two,
	// so a family that started emitting a different token cannot pass by
	// having its fixture updated to match.
	for index, row := range result.SGRuleEdges {
		switch got := anyToStringValue(row["relationship_type"]); got {
		case securityGroupSGRuleIngressRelationshipType, securityGroupSGRuleEgressRelationshipType:
		default:
			return false, fmt.Sprintf(
				"odù %q: extractor row %d carries relationship_type %q, want one of {%q, %q}; the writer screens this value against a closed two-member vocabulary, so a different token would be rejected at write time rather than materialized",
				odu.Name, index, got, securityGroupSGRuleIngressRelationshipType, securityGroupSGRuleEgressRelationshipType,
			)
		}
	}

	actual := securityGroupSGRuleRowsToExpectedEdges(result.SGRuleEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, securityGroupSGRuleFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSecurityGroupReachability reproduces the expected %d-edge SG -> rule set exactly across all %d registry type(s) and %d fact envelope(s); the ghost anchor, ghost endpoint, unknown-source, and tombstoned rules produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// securityGroupSGRuleRowsToExpectedEdges converts the extractor's SG -> rule
// rows one-for-one into the edge identity the write template MERGEs.
//
// sg_uid and rule_uid are the template's two MATCH anchors, so they are the
// edge's source and target identity; relationship_type is the MERGE's closed
// type. scope_id, generation_id and evidence_source are stamped by the WRITER
// from its own per-run intent arguments rather than carried on the
// extractor's rows, so they are not part of what this offline guard can or
// should assert; no static set can pin per-run values. Their stamping is
// covered by the writer unit test asserting the annotated row contents
// instead.
func securityGroupSGRuleRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["sg_uid"]),
			TargetEntityID:   anyToStringValue(row["rule_uid"]),
		}
		edges = append(edges, edge)
	}
	return edges
}
