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

// securityGroupRuleEndpointFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const securityGroupRuleEndpointFamily = "security_group_rule_endpoint"

// securityGroupRuleEndpointRelationshipType is the single relationship type
// the rule -> endpoint edge MERGEs: the static TO token, read off
// canonicalSecurityGroupToEdgeUpsertCypherFormat rather than derived from
// the port or family name.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const securityGroupRuleEndpointRelationshipType = "TO"

// securityGroupRuleEndpointExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const securityGroupRuleEndpointExpectedEdgesRelPath = "go/internal/ifa/testdata/securitygroup/ifa-security-group-rule-endpoint-family-expected-edges.json"

// securityGroupRuleEndpointExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func securityGroupRuleEndpointExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, securityGroupRuleEndpointExpectedEdgesRelPath)
}

// resolveSecurityGroupRuleEndpointMaterializedEdges is
// security_group_rule_endpoint's named vacuity guard (#6228).
//
// It runs the same production ExtractSecurityGroupReachability as its SG ->
// rule sibling over the shared Odù, but reads the RuleEndpointEdges row
// subset: one TO edge per fully resolved rule. What the fixture proves is
// the endpoint-resolution half — a CIDR rule lands on its recomputed
// CidrBlock uid, a prefix-list rule on its account-scoped PrefixList uid, a
// referenced-group rule on the group's CloudResource uid — while the same
// four non-producers (unscanned anchor, unscanned endpoint, unknown source,
// tombstone) each resolve to nothing.
//
// The Odù's facts are partitioned by fact kind with the same two-case switch
// the handler's own splitSecurityGroupReachabilityEnvelopes uses, mirroring
// the sibling guard: the production split is a pure kind partition, so a
// partition regression reds the handler and both guards together instead of
// letting them disagree.
//
// A quarantined fact is fatal rather than skipped, for the same reason as the
// sibling guard: a fixture that stopped decoding cannot prove the set it
// names, and the surviving facts would understate its own claim.
func resolveSecurityGroupRuleEndpointMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, securityGroupRuleEndpointFamily, odu.Name,
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
	if len(result.RuleEndpointEdges) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSecurityGroupReachability produced zero rule -> endpoint rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range result.RuleEndpointEdges {
		if got := anyToStringValue(row["target_label"]); got == "" {
			return false, fmt.Sprintf("odù %q: extractor row %d carries a blank target_label; the writer screens the label against a closed vocabulary before interpolating it into the MATCH, so a blank label would be rejected at write time rather than materialized", odu.Name, index)
		}
	}

	actual := securityGroupRuleEndpointRowsToExpectedEdges(result.RuleEndpointEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, securityGroupRuleEndpointFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSecurityGroupReachability reproduces the expected %d-edge rule -> endpoint set exactly across all %d registry type(s) and %d fact envelope(s); the ghost anchor, ghost endpoint, unknown-source, and tombstoned rules produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// securityGroupRuleEndpointRowsToExpectedEdges converts the extractor's
// rule -> endpoint rows one-for-one into the edge identity the write
// template MERGEs.
//
// rule_uid and target_uid are the template's two MATCH anchors, so they are
// the edge's source and target identity. target_label fills the MATCH
// node-label position — heterogeneous but closed-vocabulary — so it rides
// along as an asserted mutable property: the edge key cannot see it, and a
// regression resolving the right uid under the wrong label would otherwise
// pass on identity alone while the writer's MATCH no-ops live.
//
// scope_id, generation_id and evidence_source are stamped by the WRITER from
// its own per-run intent arguments rather than carried on the extractor's
// rows, so they are not part of what this offline guard can or should
// assert; no static set can pin per-run values. Their stamping is covered by
// the writer unit test asserting the annotated row contents instead.
func securityGroupRuleEndpointRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: securityGroupRuleEndpointRelationshipType,
			SourceEntityID:   anyToStringValue(row["rule_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
			Properties: map[string]string{
				"target_label": anyToStringValue(row["target_label"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
