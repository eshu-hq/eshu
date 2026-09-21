// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func endpointRelationships(
	boundary awscloud.Boundary,
	endpoint ResolverEndpoint,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(endpoint.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.HostVPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverEndpointInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"direction": strings.TrimSpace(endpoint.Direction),
			},
			SourceRecordID: id + "#vpc#" + vpcID,
		})
	}
	seen := make(map[string]struct{}, len(endpoint.SubnetIDs))
	for _, subnet := range endpoint.SubnetIDs {
		subnetID := strings.TrimSpace(subnet)
		if subnetID == "" {
			continue
		}
		if _, ok := seen[subnetID]; ok {
			continue
		}
		seen[subnetID] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverEndpointUsesSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   id + "#subnet#" + subnetID,
		})
	}
	return observations
}

func ruleRelationships(boundary awscloud.Boundary, rule ResolverRule) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(rule.ID)
	if id == "" {
		return nil
	}
	endpointID := strings.TrimSpace(rule.ResolverEndpointID)
	if endpointID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipRoute53ResolverRuleUsesEndpoint,
		SourceResourceID: id,
		TargetResourceID: endpointID,
		TargetType:       awscloud.ResourceTypeRoute53ResolverEndpoint,
		Attributes: map[string]any{
			"rule_type": strings.TrimSpace(rule.RuleType),
		},
		SourceRecordID: id + "#endpoint#" + endpointID,
	}}
}

func ruleAssociationRelationships(
	boundary awscloud.Boundary,
	association ResolverRuleAssociation,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(association.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(association.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverRuleAssociationTargetsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if ruleID := strings.TrimSpace(association.ResolverRuleID); ruleID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverRuleAssociationUsesRule,
			SourceResourceID: id,
			TargetResourceID: ruleID,
			TargetType:       awscloud.ResourceTypeRoute53ResolverRule,
			SourceRecordID:   id + "#rule#" + ruleID,
		})
	}
	return observations
}

func firewallRuleGroupAssociationRelationships(
	boundary awscloud.Boundary,
	association FirewallRuleGroupAssociation,
) []awscloud.RelationshipObservation {
	id := strings.TrimSpace(association.ID)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(association.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if groupID := strings.TrimSpace(association.FirewallRuleGroupID); groupID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup,
			SourceResourceID: id,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeRoute53ResolverFirewallRuleGroup,
			SourceRecordID:   id + "#rule-group#" + groupID,
		})
	}
	return observations
}
