// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Security-group-rule source-kind discriminators. Each emitted
// aws_security_group_rule fact normalizes its rule target to exactly one of
// these so the reducer can project a single typed reachability edge per fact
// without re-deriving which target family a rule names.
const (
	// SecurityGroupRuleSourceCIDRIPv4 marks an IPv4 CIDR rule target.
	SecurityGroupRuleSourceCIDRIPv4 = "cidr_ipv4"
	// SecurityGroupRuleSourceCIDRIPv6 marks an IPv6 CIDR rule target.
	SecurityGroupRuleSourceCIDRIPv6 = "cidr_ipv6"
	// SecurityGroupRuleSourcePrefixList marks a managed-prefix-list rule target.
	SecurityGroupRuleSourcePrefixList = "prefix_list"
	// SecurityGroupRuleSourceSecurityGroup marks a referenced-security-group
	// rule target.
	SecurityGroupRuleSourceSecurityGroup = "referenced_security_group"
	// SecurityGroupRuleSourceUnknown marks a rule whose describe response carried
	// no CIDR, prefix list, or referenced group. It preserves the rule as
	// evidence instead of dropping it so the reducer can surface incomplete
	// posture rather than silently lose a rule.
	SecurityGroupRuleSourceUnknown = "unknown"
)

// Security-group-rule directions. AWS reports direction through the is_egress
// boolean; the fact normalizes it to a stable string the reducer keys on.
const (
	// SecurityGroupRuleDirectionIngress marks an inbound rule.
	SecurityGroupRuleDirectionIngress = "ingress"
	// SecurityGroupRuleDirectionEgress marks an outbound rule.
	SecurityGroupRuleDirectionEgress = "egress"
)

// allProtocolsSentinel is the AWS IpProtocol value meaning "all protocols".
// AWS also leaves FromPort/ToPort unset for an all-protocols rule, so the fact
// derives is_all_ports from it as well.
const allProtocolsSentinel = "-1"

// internetCIDRIPv4 and internetCIDRIPv6 are the open-to-the-world CIDRs. The
// fact carries an is_internet boolean derived here so a downstream
// internet-exposure query does not re-parse CIDR strings on the hot path.
const (
	internetCIDRIPv4 = "0.0.0.0/0"
	internetCIDRIPv6 = "::/0"
)

// SecurityGroupRuleObservation describes one normalized EC2 security-group
// ingress or egress rule reported by DescribeSecurityGroupRules. It is the
// posture view of a rule: the scanner has already resolved the rule's single
// target family, so the observation carries one (SourceKind, SourceValue) pair
// rather than a bag of optional CIDR/prefix/group fields. The builder derives
// is_internet, is_all_protocols, and is_all_ports so the reducer can project
// network-reachability edges without re-deriving them.
type SecurityGroupRuleObservation struct {
	Boundary       Boundary
	RuleID         string
	GroupID        string
	GroupOwnerID   string
	IsEgress       bool
	IPProtocol     string
	FromPort       *int32
	ToPort         *int32
	CIDRIPv4       string
	CIDRIPv6       string
	PrefixListID   string
	ReferencedSG   string
	Description    string
	SourceURI      string
	SourceRecordID string
}

// NewSecurityGroupRuleEnvelope builds the durable aws_security_group_rule
// posture fact for one EC2 security-group rule. The fact is metadata-only: it
// carries identifiers, the protocol, the port range, and the normalized source,
// never a payload, secret, or policy body. It is distinct from the raw
// aws_resource security-group-rule observation; this fact is the reachability
// tuple the reducer projects into ALLOWS_INGRESS/EGRESS edges in a later slice.
func NewSecurityGroupRuleEnvelope(observation SecurityGroupRuleObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	groupID := strings.TrimSpace(observation.GroupID)
	if groupID == "" {
		return facts.Envelope{}, fmt.Errorf("aws security group rule observation requires group_id")
	}
	direction := securityGroupRuleDirection(observation.IsEgress)
	ipProtocol := strings.TrimSpace(observation.IPProtocol)
	sourceKind, sourceValue := normalizeSecurityGroupRuleSource(observation)
	ruleID := strings.TrimSpace(observation.RuleID)
	stableKey := facts.StableID(facts.AWSSecurityGroupRuleFactKind, map[string]any{
		"account_id":   observation.Boundary.AccountID,
		"direction":    direction,
		"from_port":    portValue(observation.FromPort),
		"group_id":     groupID,
		"ip_protocol":  ipProtocol,
		"region":       observation.Boundary.Region,
		"rule_id":      ruleID,
		"source_kind":  sourceKind,
		"source_value": sourceValue,
		"to_port":      portValue(observation.ToPort),
	})
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"rule_id":               ruleID,
		"group_id":              groupID,
		"group_owner_id":        strings.TrimSpace(observation.GroupOwnerID),
		"direction":             direction,
		"ip_protocol":           ipProtocol,
		"from_port":             portValue(observation.FromPort),
		"to_port":               portValue(observation.ToPort),
		"source_kind":           sourceKind,
		"source_value":          sourceValue,
		"description":           strings.TrimSpace(observation.Description),
		"is_internet":           isInternetSource(sourceKind, sourceValue),
		"is_all_protocols":      ipProtocol == allProtocolsSentinel,
		"is_all_ports":          isAllPorts(ipProtocol, observation.FromPort, observation.ToPort),
		"correlation_anchors":   securityGroupRuleAnchors(ruleID, groupID, sourceKind, sourceValue),
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSSecurityGroupRuleFactKind,
		facts.AWSSecurityGroupRuleSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, securityGroupRuleSourceID(groupID, direction, ipProtocol, sourceKind, sourceValue, observation)),
		observation.SourceURI,
		payload,
	), nil
}

func securityGroupRuleDirection(isEgress bool) string {
	if isEgress {
		return SecurityGroupRuleDirectionEgress
	}
	return SecurityGroupRuleDirectionIngress
}

// normalizeSecurityGroupRuleSource collapses the rule's optional target fields
// into one (kind, value) pair, preferring the most specific target AWS would
// populate. AWS reports exactly one target per rule, so the precedence only
// matters for malformed input; it stays deterministic so the stable key never
// flips for the same rule.
func normalizeSecurityGroupRuleSource(observation SecurityGroupRuleObservation) (string, string) {
	if value := strings.TrimSpace(observation.CIDRIPv4); value != "" {
		return SecurityGroupRuleSourceCIDRIPv4, value
	}
	if value := strings.TrimSpace(observation.CIDRIPv6); value != "" {
		return SecurityGroupRuleSourceCIDRIPv6, value
	}
	if value := strings.TrimSpace(observation.PrefixListID); value != "" {
		return SecurityGroupRuleSourcePrefixList, value
	}
	if value := strings.TrimSpace(observation.ReferencedSG); value != "" {
		return SecurityGroupRuleSourceSecurityGroup, value
	}
	return SecurityGroupRuleSourceUnknown, ""
}

// isInternetSource reports whether a rule's source opens the rule to the public
// internet. Only an exact open CIDR counts; a referenced group or prefix list
// is not internet exposure on its own.
func isInternetSource(sourceKind, sourceValue string) bool {
	switch sourceKind {
	case SecurityGroupRuleSourceCIDRIPv4:
		return sourceValue == internetCIDRIPv4
	case SecurityGroupRuleSourceCIDRIPv6:
		return sourceValue == internetCIDRIPv6
	default:
		return false
	}
}

// isAllPorts reports whether a rule spans every port. AWS encodes "all ports"
// either as the all-protocols sentinel or as an absent/explicit -1 port range.
func isAllPorts(ipProtocol string, fromPort, toPort *int32) bool {
	if ipProtocol == allProtocolsSentinel {
		return true
	}
	return portCoversAll(fromPort) && portCoversAll(toPort)
}

func portCoversAll(port *int32) bool {
	return port == nil || *port == -1
}

// portValue renders an optional port for the payload and stable key. A nil port
// (all-protocols rules omit the range) stays nil rather than collapsing to 0,
// which is a real port.
func portValue(port *int32) any {
	if port == nil {
		return nil
	}
	return *port
}

func securityGroupRuleAnchors(ruleID, groupID, sourceKind, sourceValue string) []string {
	anchors := []string{groupID}
	if ruleID != "" {
		anchors = append(anchors, ruleID)
	}
	if sourceKind == SecurityGroupRuleSourceSecurityGroup && sourceValue != "" {
		anchors = append(anchors, sourceValue)
	}
	return normalizedAnchors(nil, anchors...)
}

// securityGroupRuleSourceID builds the deterministic source record id used when
// AWS did not report a rule id (inline rules predating the rule-id era). It
// keys on the same identity the stable key uses so replays of the same rule
// resolve to one source record.
func securityGroupRuleSourceID(groupID, direction, ipProtocol, sourceKind, sourceValue string, observation SecurityGroupRuleObservation) string {
	if ruleID := strings.TrimSpace(observation.RuleID); ruleID != "" {
		return ruleID
	}
	parts := []string{
		groupID,
		direction,
		ipProtocol,
		fmt.Sprintf("%v", portValue(observation.FromPort)),
		fmt.Sprintf("%v", portValue(observation.ToPort)),
		sourceKind,
		sourceValue,
	}
	return strings.Join(parts, "#")
}
