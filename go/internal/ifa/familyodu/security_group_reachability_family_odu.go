// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// The security-group reachability family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// ONE Odù serves TWO families: security_group_sg_rule (SecurityGroup ->
// SecurityGroupRule ALLOWS_INGRESS / ALLOWS_EGRESS) and
// security_group_rule_endpoint (SecurityGroupRule -[:TO]-> endpoint). Both
// guards run the same production ExtractSecurityGroupReachability over these
// facts and read different row subsets (SGRuleEdges vs RuleEndpointEdges), so
// splitting the facts would duplicate the substrate: every live rule yields
// one node plus one edge of each family, and neither family's rows exist
// without the other's. The roster carries one entry per family naming this
// same Odù.
//
// A DIRECT-materialization family pair: the reducer writes them straight to
// cypher.SecurityGroupReachabilityWriter through the
// WriteSecurityGroupSGRuleEdges / WriteSecurityGroupRuleEndpointEdges ports.
// The SG -> rule relationship type comes from the rule direction over the
// closed {ingress, egress} -> {ALLOWS_INGRESS, ALLOWS_EGRESS} vocabulary; the
// rule -> endpoint type is the static TO token while the target-node label
// varies over the closed {CidrBlock, PrefixList, CloudResource} endpoint
// vocabulary. Read all three off the write templates and the vocabularies
// they screen against, never off the port or family names.
//
// Facts are built as typed awsv1.Resource / awsv1.SecurityGroupRule values
// and encoded through factschema.EncodeAWSResource /
// factschema.EncodeAWSSecurityGroupRule, never as hand-built maps (Contract
// System v1).
//
// Scope note: SG anchors materialize from aws_resource facts, so this Odù
// carries the three scanned group facts the join index resolves against,
// plus eight rule facts. A group id with no scanned fact never resolves;
// an endpoint referencing an unscanned group degrades the same way.

const (
	// SecurityGroupReachabilityFamilyOduName is this Odù's catalog name, the
	// ref a materialized_edges:security_group_sg_rule or
	// materialized_edges:security_group_rule_endpoint coverage row would name
	// to resolve through it.
	SecurityGroupReachabilityFamilyOduName = "odu:ifa-security-group-reachability-family"

	// securityGroupReachabilityFamilyScopeID is the single AWS account scope
	// every fact in this Odù belongs to. The reducer handler loads one scope
	// generation's facts, so a fixture spanning scopes would not mirror any
	// real intent.
	//
	// The scope is deliberately NOT any sibling cassette's scope.
	// Scopes carry one ACTIVE generation; driving a second generation into
	// an occupied scope supersedes the first family's generation, its
	// handler never runs, and the sibling exact-set assert fails with zero
	// edges — diagnosed live 2026-09-27 on the shared scope during the
	// iam_can_assume drive. One live generation per scope per cell is the
	// contract.
	securityGroupReachabilityFamilyScopeID = "aws:eshu-fixture-security-group-reachability-account"

	// securityGroupReachabilityFamilyAccountID is the account every fact
	// below shares. Synthetic, matching the collector's own redaction
	// posture.
	securityGroupReachabilityFamilyAccountID = "123456789012"

	// securityGroupReachabilityFamilyRegion is the region every fact below
	// carries. The join index folds account/region into the anchor uid, so
	// anchors and rules agree by construction.
	securityGroupReachabilityFamilyRegion = "us-east-1"

	// securityGroupReachabilityFamilyGenerationID is the one scope
	// generation the Odù replays. The reducer's handler loads a single scope
	// generation's facts, so every fact below shares it.
	securityGroupReachabilityFamilyGenerationID = "gen-ifa-security-group-reachability-family-1"

	// securityGroupReachabilityFamilyCollectorKind mirrors what the awscloud
	// collector stamps on both fact kinds, so the Odù describes the same
	// envelopes a live generation would carry rather than agreeing only on
	// the payload.
	securityGroupReachabilityFamilyCollectorKind = "aws"

	// securityGroupReachabilityFamilySourceConfidence marks these facts as
	// directly observed, the posture a scanner-emitted resource carries.
	securityGroupReachabilityFamilySourceConfidence = "observed"
)

// The fixture group ids. web-sg and db-sg anchor edge-producing rules;
// ref-sg is scanned so the referenced-security-group rule resolves its
// endpoint; ghost-sg is deliberately NEVER scanned, so rules anchored to it
// (or referencing it) prove graceful degradation instead of dangling.
const (
	securityGroupReachabilityFamilyWebSG   = "sg-0abc123def456789a"
	securityGroupReachabilityFamilyDbSG    = "sg-0def123456789abcd"
	securityGroupReachabilityFamilyRefSG   = "sg-0ref123456789abcd"
	securityGroupReachabilityFamilyGhostSG = "sg-9zzz9999999999999"
	securityGroupReachabilityFamilyPrefix  = "pl-0abc123def456789a"
)

// securityGroupReachabilityFamilyRuleFixture describes one
// aws_security_group_rule fact in the Odù.
type securityGroupReachabilityFamilyRuleFixture struct {
	// GroupID is the bare security-group id the rule belongs to. It must be
	// a scanned anchor for the rule to resolve, unless the fixture proves
	// the unresolved-anchor skip.
	GroupID string
	// Direction is the normalized rule direction (ingress or egress). It
	// selects the closed-vocabulary SG -> rule relationship type.
	Direction string
	// IPProtocol is the rule's IP protocol token.
	IPProtocol string
	// FromPort and ToPort bound the rule's port range. Nil means an
	// all-ports rule that omits the range.
	FromPort *int32
	ToPort   *int32
	// SourceKind selects endpoint resolution (cidr_ipv4, cidr_ipv6,
	// prefix_list, referenced_security_group, or unknown).
	SourceKind string
	// SourceValue is the endpoint value for its kind (a CIDR, a prefix-list
	// id, or a referenced group id). Empty only for the unknown kind.
	SourceValue string
	// Tombstone marks a rule a later generation retracted: it materializes
	// nothing, and the prior-generation retract owns its old edges.
	Tombstone bool
}

// int32Ptr boxes a port literal for the fixture table.
func int32Ptr(value int32) *int32 {
	return &value
}

// securityGroupReachabilityFamilyAnchors are the scanned node substrate:
// the two anchors edge-producing rules hang off, plus the referenced group
// the cross-group rule resolves through. ghost-sg is deliberately absent.
var securityGroupReachabilityFamilyAnchors = []string{
	securityGroupReachabilityFamilyWebSG,
	securityGroupReachabilityFamilyDbSG,
	securityGroupReachabilityFamilyRefSG,
}

// securityGroupReachabilityFamilyRules are the rules: five edge producers
// covering both SG -> rule directions and all three endpoint labels (plus
// the IPv6 CIDR family), and three deliberate non-producers covering the
// unresolved-anchor, unresolved-endpoint, unknown-source, and tombstone
// branches.
//
// The non-producers are the load-bearing half. The extractor never
// fabricates an endpoint — an unscanned anchor, an unscanned referenced
// group, a rule with no usable source, and a tombstoned rule each resolve
// to nothing — and the writer's MATCH clauses would no-op on a missing node
// anyway. Without them, a regression that started emitting an edge for every
// rule regardless of resolution would still reproduce the expected set
// exactly and this fixture would report green.
var securityGroupReachabilityFamilyRules = []securityGroupReachabilityFamilyRuleFixture{
	{
		// EDGE: ingress ssh from an IPv4 CIDR. SG edge ALLOWS_INGRESS,
		// TO edge onto CidrBlock.
		GroupID:     securityGroupReachabilityFamilyWebSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(22),
		ToPort:      int32Ptr(22),
		SourceKind:  "cidr_ipv4",
		SourceValue: "10.0.0.0/8",
	},
	{
		// EDGE: egress https to an IPv4 CIDR. SG edge ALLOWS_EGRESS,
		// TO edge onto CidrBlock — the direction that proves the second
		// closed-vocabulary SG -> rule type.
		GroupID:     securityGroupReachabilityFamilyWebSG,
		Direction:   "egress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(443),
		ToPort:      int32Ptr(443),
		SourceKind:  "cidr_ipv4",
		SourceValue: "10.0.0.0/8",
	},
	{
		// EDGE: ingress http from a prefix list. SG edge ALLOWS_INGRESS,
		// TO edge onto PrefixList.
		GroupID:     securityGroupReachabilityFamilyDbSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(80),
		ToPort:      int32Ptr(80),
		SourceKind:  "prefix_list",
		SourceValue: securityGroupReachabilityFamilyPrefix,
	},
	{
		// EDGE: ingress postgres from a referenced security group. SG edge
		// ALLOWS_INGRESS, TO edge onto the referenced group's
		// CloudResource node — the endpoint that resolves through the same
		// join index as the anchor rather than a recomputed uid.
		GroupID:     securityGroupReachabilityFamilyDbSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(5432),
		ToPort:      int32Ptr(5432),
		SourceKind:  "referenced_security_group",
		SourceValue: securityGroupReachabilityFamilyRefSG,
	},
	{
		// EDGE: ingress dns from an IPv6 CIDR. SG edge ALLOWS_INGRESS,
		// TO edge onto the IPv6 CidrBlock — the second address family the
		// canonicalizer folds.
		GroupID:     securityGroupReachabilityFamilyDbSG,
		Direction:   "ingress",
		IPProtocol:  "udp",
		FromPort:    int32Ptr(53),
		ToPort:      int32Ptr(53),
		SourceKind:  "cidr_ipv6",
		SourceValue: "2001:db8::/32",
	},
	{
		// NO EDGE: anchored to a group the scope never scanned — the
		// unresolved-anchor skip.
		GroupID:     securityGroupReachabilityFamilyGhostSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(22),
		ToPort:      int32Ptr(22),
		SourceKind:  "cidr_ipv4",
		SourceValue: "10.0.0.0/8",
	},
	{
		// NO EDGE: references a group the scope never scanned — the
		// unresolved-endpoint skip, distinct from the anchor skip above.
		GroupID:     securityGroupReachabilityFamilyWebSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(3306),
		ToPort:      int32Ptr(3306),
		SourceKind:  "referenced_security_group",
		SourceValue: securityGroupReachabilityFamilyGhostSG,
	},
	{
		// NO EDGE: reported no usable source — the unknown-source skip.
		GroupID:     securityGroupReachabilityFamilyWebSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(25),
		ToPort:      int32Ptr(25),
		SourceKind:  "unknown",
		SourceValue: "",
	},
	{
		// NO EDGE: retracted by a later generation — the tombstone skip.
		// The prior-generation retract owns any edges a prior live rule
		// wrote; this generation materializes nothing for it.
		GroupID:     securityGroupReachabilityFamilyWebSG,
		Direction:   "ingress",
		IPProtocol:  "tcp",
		FromPort:    int32Ptr(3389),
		ToPort:      int32Ptr(3389),
		SourceKind:  "cidr_ipv4",
		SourceValue: "10.0.0.0/8",
		Tombstone:   true,
	},
}

// securityGroupReachabilityFamilyStableFactKey derives one fact's durable
// dedup key from the same identity inputs the collector keys its StableID
// by, in a readable fixture-local format rather than a hex digest, instead
// of hand-typing a string the expected-edge fixture cannot independently
// check.
//
// The rule half MUST carry the full rule identity, not just the group: the
// fixture hangs five edge-producing rules off two groups (plus four
// non-producers sharing those groups), so keying on group alone would
// collapse distinct rules: live ingest would drive fewer facts than claimed
// and negative controls would never reach the extractor. Key narrowness is
// invisible to every other check in this family (the offline guard runs over
// the compiled facts, never through ingest dedup), so it is pinned here and
// by the uniqueness test below rather than assumed from the derivation
// shape.
func securityGroupReachabilityFamilyStableFactKey(factKind, identity string) string {
	return fmt.Sprintf(
		"aws:%s:us-east-1:%s:%s",
		securityGroupReachabilityFamilyAccountID,
		factKind, identity,
	)
}

// securityGroupReachabilityFamilyRuleIdentity renders one rule fixture's
// stable-key identity from the same tokens the rule uid folds: direction,
// protocol, normalized port range, and source.
func securityGroupReachabilityFamilyRuleIdentity(fixture securityGroupReachabilityFamilyRuleFixture) string {
	from, to := "", ""
	if fixture.FromPort != nil {
		from = fmt.Sprintf("%d", *fixture.FromPort)
	}
	if fixture.ToPort != nil {
		to = fmt.Sprintf("%d", *fixture.ToPort)
	}
	return fmt.Sprintf(
		"%s:%s:%s:%s:%s:%s:%s",
		fixture.GroupID, fixture.Direction, fixture.IPProtocol,
		from, to, fixture.SourceKind, fixture.SourceValue,
	)
}

// SecurityGroupReachabilityFamilyOdu builds the cataloged Odù shared by the
// security_group_sg_rule and security_group_rule_endpoint
// direct-materialization families.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. It panics on an encode
// failure for the same reason S3LogsToFamilyOdu does: a failure means the
// payload contract moved under a committed fixture, and every coverage claim
// built on it is already void.
func SecurityGroupReachabilityFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(securityGroupReachabilityFamilyAnchors)+len(securityGroupReachabilityFamilyRules))
	for _, groupID := range securityGroupReachabilityFamilyAnchors {
		resource := awsv1.Resource{
			AccountID:    securityGroupReachabilityFamilyAccountID,
			ResourceID:   groupID,
			Region:       securityGroupReachabilityFamilyRegion,
			ResourceType: awsv1.ResourceTypeEC2SecurityGroup,
		}
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				SecurityGroupReachabilityFamilyOduName, groupID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          securityGroupReachabilityFamilyScopeID,
			GenerationID:     securityGroupReachabilityFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    securityGroupReachabilityFamilyStableFactKey(cloud.AWSResourceFactKind, groupID),
			SchemaVersion:    cloud.AWSResourceSchemaVersion,
			CollectorKind:    securityGroupReachabilityFamilyCollectorKind,
			SourceConfidence: securityGroupReachabilityFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for _, fixture := range securityGroupReachabilityFamilyRules {
		rule := awsv1.SecurityGroupRule{
			AccountID:   securityGroupReachabilityFamilyAccountID,
			Region:      securityGroupReachabilityFamilyRegion,
			GroupID:     fixture.GroupID,
			Direction:   fixture.Direction,
			IPProtocol:  fixture.IPProtocol,
			FromPort:    fixture.FromPort,
			ToPort:      fixture.ToPort,
			SourceKind:  fixture.SourceKind,
			SourceValue: fixture.SourceValue,
		}
		payload, err := factschema.EncodeAWSSecurityGroupRule(rule)
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_security_group_rule payload for %q: %v",
				SecurityGroupReachabilityFamilyOduName, securityGroupReachabilityFamilyRuleIdentity(fixture), err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          securityGroupReachabilityFamilyScopeID,
			GenerationID:     securityGroupReachabilityFamilyGenerationID,
			FactKind:         cloud.AWSSecurityGroupRuleFactKind,
			StableFactKey:    securityGroupReachabilityFamilyStableFactKey(cloud.AWSSecurityGroupRuleFactKind, securityGroupReachabilityFamilyRuleIdentity(fixture)),
			SchemaVersion:    cloud.AWSSecurityGroupRuleSchemaVersion,
			CollectorKind:    securityGroupReachabilityFamilyCollectorKind,
			SourceConfidence: securityGroupReachabilityFamilySourceConfidence,
			Payload:          payload,
			IsTombstone:      fixture.Tombstone,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: SecurityGroupReachabilityFamilyOduName, Facts: factsForOdu},
		Detail: "twelve facts for the direct-materialization security-group reachability family pair: three aws_resource security-group node facts (the two anchors edge-producing rules hang off plus the referenced group the cross-group rule resolves through) and nine aws_security_group_rule facts (five edge producers covering both SG -> rule directions, all three TO endpoint labels, and both CIDR families, plus four deliberate non-producers covering the unresolved-anchor, unresolved-endpoint, unknown-source, and tombstone branches), so both expected sets prove resolution and restraint",
	}
}
