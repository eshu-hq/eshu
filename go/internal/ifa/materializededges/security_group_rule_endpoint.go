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

// securityGroupRuleEndpointTargetLabelByRule binds each of the five live
// rules to its expected endpoint label, keyed by the rule_uid the extractor
// recomputes. The shared expected-edges fixture cannot assert the label —
// the writer MERGEs on its two endpoint nodes alone and target_label only
// fills the MATCH node-label position — so the guard checks it here, beside
// the uid comparison: a regression resolving the right uid under the wrong
// label fails hermetically instead of passing through to the live tier.
// Measured off the extractor over the shared Odù; an unknown rule_uid fails
// closed so a new resolving rule must land here deliberately.
var securityGroupRuleEndpointTargetLabelByRule = map[string]string{
	"0a3afef6f922fb2707a353db2d131190432d0120fe17988f3b5d0a69768c8161": "CidrBlock",
	"2bd0a21bf7c2da764fafd5cd8bfc5069ec72e952de9be18479904ac2c1c24f07": "CidrBlock",
	"72b8b0b553d9576dabcbf58294b3fb6051296bb284086a795c9208c113af2251": "CloudResource",
	"91f3f557f98ef57990c79f8d6a3514051aa1b125c217ac5dc43c0c8e715e3973": "CidrBlock",
	"957b5c8d30dff26cc1d05efe35545a3d5b1c776eee2e577dda138d6a2b6a9ecd": "PrefixList",
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
	if problem := checkSecurityGroupRuleEndpointTargetLabels(odu.Name, result.RuleEndpointEdges); problem != "" {
		return false, problem
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

// checkSecurityGroupRuleEndpointTargetLabels binds each extractor row's
// target_label to its per-rule expectation from
// securityGroupRuleEndpointTargetLabelByRule. The shared fixture compares
// edge identity only, so without this a regression resolving the right uid
// under the wrong label would pass hermetically. An unlisted rule_uid fails
// closed: a newly resolving rule must land in the map deliberately.
func checkSecurityGroupRuleEndpointTargetLabels(oduName string, rows []map[string]any) string {
	for index, row := range rows {
		ruleUID := anyToStringValue(row["rule_uid"])
		want, known := securityGroupRuleEndpointTargetLabelByRule[ruleUID]
		if !known {
			return fmt.Sprintf("odù %q: extractor row %d names an unlisted rule_uid; the per-rule label map must gain the new rule with its expected endpoint label, or a new rule is resolving that the fixture never meant to cover", oduName, index)
		}
		if got := anyToStringValue(row["target_label"]); got != want {
			return fmt.Sprintf("odù %q: extractor row %d resolves its endpoint under label %q, want %q; the writer's MATCH (target:LABEL) would no-op a wrong-label write, so the edge identity alone cannot see this regression", oduName, index, got, want)
		}
	}
	return ""
}

// securityGroupRuleEndpointRowsToExpectedEdges converts the extractor's
// rule -> endpoint rows one-for-one into the edge identity the write
// template MERGEs.
//
// rule_uid and target_uid are the template's two MATCH anchors, so they are
// the edge's source and target identity. target_label fills the MATCH
// node-label position — heterogeneous but closed-vocabulary — and stays a
// row-level routing field only: the production TO edges never carried it
// as a graph property (the writer SETs scope/generation/evidence only),
// so the shared expected-edges fixture cannot assert it and this conversion
// does not carry it either. A regression resolving the right uid under the
// wrong label still fails: the writer's MATCH (target:LABEL) no-ops the
// write, and the live exact-set assert reports the edge missing.
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
		}
		edges = append(edges, edge)
	}
	return edges
}
