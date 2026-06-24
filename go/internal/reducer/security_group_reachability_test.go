// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sgAccountID and sgRegion are the fixed account/region scope every fixture rule
// and its anchor security group share, so the in-memory join index resolves the
// anchor under the trust boundary.
const (
	sgAccountID = "111122223333"
	sgRegion    = "us-east-1"
)

// securityGroupAnchorEnvelope builds an aws_resource fact for the security group
// a rule is attached to, so the reachability extractor can resolve the rule's
// SG anchor to a materialized CloudResource uid through the shared join index.
func securityGroupAnchorEnvelope(groupID string) facts.Envelope {
	return resourceEnvelope(sgAccountID, sgRegion, "aws_ec2_security_group", groupID, "")
}

// sgReachabilityRulePayload builds an aws_security_group_rule posture payload for
// one normalized reachability tuple. The port arguments mirror the scanner's
// nullable int32 ports; a nil pointer means an all-protocols/all-ports rule.
func sgReachabilityRulePayload(groupID, direction, ipProtocol string, fromPort, toPort any, sourceKind, sourceValue string) map[string]any {
	return map[string]any{
		"account_id":   sgAccountID,
		"region":       sgRegion,
		"service_kind": "ec2",
		"group_id":     groupID,
		"direction":    direction,
		"ip_protocol":  ipProtocol,
		"from_port":    fromPort,
		"to_port":      toPort,
		"source_kind":  sourceKind,
		"source_value": sourceValue,
		"is_internet":  sourceValue == "0.0.0.0/0" || sourceValue == "::/0",
	}
}

func sgReachabilityRuleEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSSecurityGroupRuleFactKind,
		Payload:  payload,
	}
}

// TestSecurityGroupRulePortNormalizationIsTypeStable pins the most dangerous
// correctness trap: the rule node uid folds the port range, but ports arrive as
// int32 in tests and float64 after a Postgres JSON roundtrip in production. The
// normalized token MUST be identical across both representations or the same
// rule would key two different nodes between local tests and prod.
func TestSecurityGroupRulePortNormalizationIsTypeStable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"int32", int32(22), "22"},
		{"int", 22, "22"},
		{"int64", int64(22), "22"},
		{"float64 (json roundtrip)", float64(22), "22"},
		{"nil (all ports)", nil, ""},
		{"negative one all ports", int32(-1), "-1"},
		{"float64 negative one", float64(-1), "-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeSecurityGroupRulePort(tc.in); got != tc.want {
				t.Fatalf("normalizeSecurityGroupRulePort(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSecurityGroupRuleUIDIsPortPrecise proves Option D's port precision: two
// rules that differ only by port range key two distinct rule nodes, while the
// same rule keys one node regardless of port value type.
func TestSecurityGroupRuleUIDIsPortPrecise(t *testing.T) {
	t.Parallel()

	sgUID := cloudResourceUID(sgAccountID, sgRegion, "aws_ec2_security_group", "sg-0abc")

	port22 := securityGroupRuleUID(sgUID, "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")
	port443 := securityGroupRuleUID(sgUID, "ingress", "tcp", int32(443), int32(443), "cidr_ipv4", "10.0.0.0/8")
	if port22 == port443 {
		t.Fatal("two ports must key two distinct SecurityGroupRule nodes")
	}

	// int32 vs float64 (json roundtrip) must collapse to one node.
	port22Float := securityGroupRuleUID(sgUID, "ingress", "tcp", float64(22), float64(22), "cidr_ipv4", "10.0.0.0/8")
	if port22 != port22Float {
		t.Fatalf("port type must not change the uid: int32=%s float64=%s", port22, port22Float)
	}

	// Direction must change identity (ingress vs egress are different rules).
	egress := securityGroupRuleUID(sgUID, "egress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")
	if port22 == egress {
		t.Fatal("ingress and egress must key distinct rule nodes")
	}
}

// TestExtractSecurityGroupReachabilityResolvesCIDREndpoint proves the happy path:
// a single ingress CIDR rule whose SG anchor resolves produces exactly one rule
// node, one ALLOWS_INGRESS edge (SG->rule), and one TO edge (rule->CidrBlock).
func TestExtractSecurityGroupReachabilityResolvesCIDREndpoint(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})

	if len(result.RuleNodes) != 1 {
		t.Fatalf("rule nodes = %d, want 1", len(result.RuleNodes))
	}
	if len(result.SGRuleEdges) != 1 {
		t.Fatalf("SG->rule edges = %d, want 1", len(result.SGRuleEdges))
	}
	if len(result.RuleEndpointEdges) != 1 {
		t.Fatalf("rule->endpoint edges = %d, want 1", len(result.RuleEndpointEdges))
	}

	node := result.RuleNodes[0]
	sgUID := cloudResourceUID(sgAccountID, sgRegion, "aws_ec2_security_group", "sg-0abc")
	wantRuleUID := securityGroupRuleUID(sgUID, "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")
	if node["uid"] != wantRuleUID {
		t.Fatalf("rule uid = %v, want %v", node["uid"], wantRuleUID)
	}
	if node["direction"] != "ingress" || node["ip_protocol"] != "tcp" {
		t.Fatalf("rule node props wrong: %v", node)
	}
	if node["from_port"] != "22" || node["to_port"] != "22" {
		t.Fatalf("rule node ports must be normalized strings: %v", node)
	}

	sgEdge := result.SGRuleEdges[0]
	if sgEdge["relationship_type"] != "ALLOWS_INGRESS" {
		t.Fatalf("SG edge relationship_type = %v, want ALLOWS_INGRESS", sgEdge["relationship_type"])
	}
	if sgEdge["sg_uid"] != sgUID || sgEdge["rule_uid"] != wantRuleUID {
		t.Fatalf("SG edge endpoints wrong: %v", sgEdge)
	}

	toEdge := result.RuleEndpointEdges[0]
	if toEdge["target_label"] != "CidrBlock" {
		t.Fatalf("TO edge target_label = %v, want CidrBlock", toEdge["target_label"])
	}
	wantEndpointUID := cidrBlockUID("10.0.0.0/8", "ipv4")
	if toEdge["rule_uid"] != wantRuleUID || toEdge["target_uid"] != wantEndpointUID {
		t.Fatalf("TO edge endpoints wrong: %v", toEdge)
	}
}

// TestExtractSecurityGroupReachabilityEgressUsesAllowsEgress proves direction
// drives the relationship type: an egress rule emits ALLOWS_EGRESS.
func TestExtractSecurityGroupReachabilityEgressUsesAllowsEgress(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "egress", "tcp", int32(443), int32(443), "cidr_ipv4", "0.0.0.0/0",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})
	if len(result.SGRuleEdges) != 1 {
		t.Fatalf("SG->rule edges = %d, want 1", len(result.SGRuleEdges))
	}
	if result.SGRuleEdges[0]["relationship_type"] != "ALLOWS_EGRESS" {
		t.Fatalf("egress rule must use ALLOWS_EGRESS, got %v", result.SGRuleEdges[0]["relationship_type"])
	}
	if result.RuleNodes[0]["is_internet"] != true {
		t.Fatalf("0.0.0.0/0 egress rule node must be is_internet=true: %v", result.RuleNodes[0])
	}
}

// TestExtractSecurityGroupReachabilityReferencedSGEndpoint proves a referenced
// security group resolves its endpoint to the CloudResource node of the named
// group (same account/region), with target_label CloudResource.
func TestExtractSecurityGroupReachabilityReferencedSGEndpoint(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	referenced := securityGroupAnchorEnvelope("sg-0def") // the referenced group is itself a scanned resource
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(5432), int32(5432), "referenced_security_group", "sg-0def",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor, referenced}, []facts.Envelope{rule})
	if len(result.RuleEndpointEdges) != 1 {
		t.Fatalf("rule->endpoint edges = %d, want 1", len(result.RuleEndpointEdges))
	}
	toEdge := result.RuleEndpointEdges[0]
	if toEdge["target_label"] != "CloudResource" {
		t.Fatalf("referenced SG endpoint target_label = %v, want CloudResource", toEdge["target_label"])
	}
	wantTargetUID := cloudResourceUID(sgAccountID, sgRegion, "aws_ec2_security_group", "sg-0def")
	if toEdge["target_uid"] != wantTargetUID {
		t.Fatalf("referenced SG endpoint target_uid = %v, want %v", toEdge["target_uid"], wantTargetUID)
	}
}

// TestExtractSecurityGroupReachabilityPrefixListEndpoint proves a prefix-list
// rule resolves to a PrefixList node uid.
func TestExtractSecurityGroupReachabilityPrefixListEndpoint(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(80), int32(80), "prefix_list", "pl-1234",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})
	if len(result.RuleEndpointEdges) != 1 {
		t.Fatalf("rule->endpoint edges = %d, want 1", len(result.RuleEndpointEdges))
	}
	toEdge := result.RuleEndpointEdges[0]
	if toEdge["target_label"] != "PrefixList" {
		t.Fatalf("prefix list endpoint target_label = %v, want PrefixList", toEdge["target_label"])
	}
	if toEdge["target_uid"] != prefixListUID(sgAccountID, sgRegion, "pl-1234") {
		t.Fatalf("prefix list endpoint target_uid wrong: %v", toEdge)
	}
}

// TestExtractSecurityGroupReachabilityUnresolvedAnchorSkips proves a rule whose
// SG anchor was not scanned in this generation produces no node and no edges,
// and is counted as skipped — no dangle, no fabrication.
func TestExtractSecurityGroupReachabilityUnresolvedAnchorSkips(t *testing.T) {
	t.Parallel()

	// No anchor resource for sg-0abc in the resource facts.
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	result := ExtractSecurityGroupReachability(nil, []facts.Envelope{rule})
	if len(result.RuleNodes) != 0 || len(result.SGRuleEdges) != 0 || len(result.RuleEndpointEdges) != 0 {
		t.Fatalf("unresolved anchor must produce nothing: %+v", result)
	}
	if result.Tally.skippedUnresolvedAnchor != 1 {
		t.Fatalf("skippedUnresolvedAnchor = %d, want 1", result.Tally.skippedUnresolvedAnchor)
	}
}

// TestExtractSecurityGroupReachabilityReferencedSGEndpointUnresolvedSkips proves
// a referenced group that was not scanned is skipped (no fabricated endpoint),
// while the rule node still has a resolvable anchor.
func TestExtractSecurityGroupReachabilityReferencedSGEndpointUnresolvedSkips(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	// sg-0def is referenced but never scanned, so the endpoint cannot resolve.
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(5432), int32(5432), "referenced_security_group", "sg-0def",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})
	if len(result.RuleNodes) != 0 || len(result.SGRuleEdges) != 0 || len(result.RuleEndpointEdges) != 0 {
		t.Fatalf("unresolved endpoint must produce nothing: %+v", result)
	}
	if result.Tally.skippedUnresolvedEndpoint != 1 {
		t.Fatalf("skippedUnresolvedEndpoint = %d, want 1", result.Tally.skippedUnresolvedEndpoint)
	}
}

// TestExtractSecurityGroupReachabilityUnknownSourceSkips proves an unknown source
// kind (no CIDR/prefix/group reported) materializes no node and no edge.
func TestExtractSecurityGroupReachabilityUnknownSourceSkips(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "-1", nil, nil, "unknown", "",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})
	if len(result.RuleNodes) != 0 || len(result.RuleEndpointEdges) != 0 {
		t.Fatalf("unknown source must produce no node/endpoint edge: %+v", result)
	}
	if result.Tally.skippedUnknownSource != 1 {
		t.Fatalf("skippedUnknownSource = %d, want 1", result.Tally.skippedUnknownSource)
	}
}

// TestExtractSecurityGroupReachabilityPerPortDistinctness proves two ports on the
// same SG/endpoint produce two rule nodes and two edge pairs (Option D port
// precision end to end).
func TestExtractSecurityGroupReachabilityPerPortDistinctness(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule22 := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	rule443 := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(443), int32(443), "cidr_ipv4", "10.0.0.0/8",
	))

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule22, rule443})
	if len(result.RuleNodes) != 2 {
		t.Fatalf("two ports must produce two rule nodes, got %d", len(result.RuleNodes))
	}
	if len(result.SGRuleEdges) != 2 || len(result.RuleEndpointEdges) != 2 {
		t.Fatalf("two ports must produce two edge pairs: sg=%d to=%d", len(result.SGRuleEdges), len(result.RuleEndpointEdges))
	}
}

// TestExtractSecurityGroupReachabilityIdempotentDuplicateRule proves a duplicate
// rule fact converges on one node and one edge pair (idempotent extraction).
func TestExtractSecurityGroupReachabilityIdempotentDuplicateRule(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRulePayload("sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")
	dup := sgReachabilityRulePayload("sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")

	result := ExtractSecurityGroupReachability(
		[]facts.Envelope{anchor},
		[]facts.Envelope{sgReachabilityRuleEnvelope(rule), sgReachabilityRuleEnvelope(dup)},
	)
	if len(result.RuleNodes) != 1 || len(result.SGRuleEdges) != 1 || len(result.RuleEndpointEdges) != 1 {
		t.Fatalf("duplicate rule must converge: nodes=%d sg=%d to=%d",
			len(result.RuleNodes), len(result.SGRuleEdges), len(result.RuleEndpointEdges))
	}
}

// TestExtractSecurityGroupReachabilityTombstoneSkips proves a tombstoned rule
// fact grants no reachability and produces nothing.
func TestExtractSecurityGroupReachabilityTombstoneSkips(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	rule.IsTombstone = true

	result := ExtractSecurityGroupReachability([]facts.Envelope{anchor}, []facts.Envelope{rule})
	if len(result.RuleNodes) != 0 || len(result.SGRuleEdges) != 0 || len(result.RuleEndpointEdges) != 0 {
		t.Fatalf("tombstoned rule must produce nothing: %+v", result)
	}
}

// TestExtractSecurityGroupReachabilityEmptyIsNoOp proves an empty generation is a
// clean no-op.
func TestExtractSecurityGroupReachabilityEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	result := ExtractSecurityGroupReachability(nil, nil)
	if len(result.RuleNodes) != 0 || len(result.SGRuleEdges) != 0 || len(result.RuleEndpointEdges) != 0 {
		t.Fatalf("empty generation must produce nothing: %+v", result)
	}
}
