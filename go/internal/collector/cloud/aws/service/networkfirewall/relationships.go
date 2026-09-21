// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkfirewall

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// firewallRelationships projects a firewall's reported VPC placement, subnet
// mappings, and firewall policy reference. Each edge is emitted only when AWS
// reports both endpoints. The VPC and subnet targets are bare ids that resolve
// to EC2-owned resources; the policy target is the policy ARN.
func firewallRelationships(boundary aws.Boundary, firewall Firewall) []aws.RelationshipObservation {
	sourceARN := strings.TrimSpace(firewall.ARN)
	sourceID := firstNonEmpty(sourceARN, firewall.ID, firewall.Name)
	if sourceID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(firewall.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkFirewallFirewallInVPC,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   sourceID + "#vpc#" + vpcID,
		})
	}

	for _, subnetID := range dedupeStrings(firewall.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkFirewallFirewallUsesSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "#subnet#" + subnetID,
		})
	}

	if policyARN := strings.TrimSpace(firewall.FirewallPolicyARN); policyARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkFirewallFirewallUsesPolicy,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: policyARN,
			TargetARN:        policyARN,
			TargetType:       aws.ResourceTypeNetworkFirewallPolicy,
			SourceRecordID:   sourceID + "#policy#" + policyARN,
		})
	}

	return observations
}

// policyRelationships projects a firewall policy's references to its rule
// groups and TLS inspection configuration by ARN. The references are reported
// by the policy without exposing any rule body.
func policyRelationships(boundary aws.Boundary, policy FirewallPolicy) []aws.RelationshipObservation {
	sourceARN := strings.TrimSpace(policy.ARN)
	sourceID := firstNonEmpty(sourceARN, policy.ID, policy.Name)
	if sourceID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation

	for _, ruleGroupARN := range dedupeStrings(policy.RuleGroupARNs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkFirewallPolicyUsesRuleGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: ruleGroupARN,
			TargetARN:        ruleGroupARN,
			TargetType:       aws.ResourceTypeNetworkFirewallRuleGroup,
			SourceRecordID:   sourceID + "#rule-group#" + ruleGroupARN,
		})
	}

	if tlsARN := strings.TrimSpace(policy.TLSInspectionConfigurationARN); tlsARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkFirewallPolicyUsesTLSInspectionConfiguration,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: tlsARN,
			TargetARN:        tlsARN,
			TargetType:       aws.ResourceTypeNetworkFirewallTLSInspectionConfiguration,
			SourceRecordID:   sourceID + "#tls-inspection-configuration#" + tlsARN,
		})
	}

	return observations
}

// relationshipEnvelopes converts relationship observations into durable
// envelopes, returning the first construction error so a malformed edge fails
// the scan instead of being silently dropped.
func relationshipEnvelopes(observations []aws.RelationshipObservation) ([]facts.Envelope, error) {
	if len(observations) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(observations))
	for _, observation := range observations {
		envelope, err := aws.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
