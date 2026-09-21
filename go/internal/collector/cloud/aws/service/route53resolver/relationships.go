// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func endpointRelationships(
	boundary aws.Boundary,
	endpoint ResolverEndpoint,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(endpoint.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.HostVPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverEndpointInVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
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
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverEndpointUsesSubnet,
			SourceResourceID: id,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   id + "#subnet#" + subnetID,
		})
	}
	return observations
}

func ruleRelationships(boundary aws.Boundary, rule ResolverRule) []aws.RelationshipObservation {
	id := strings.TrimSpace(rule.ID)
	if id == "" {
		return nil
	}
	endpointID := strings.TrimSpace(rule.ResolverEndpointID)
	if endpointID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipRoute53ResolverRuleUsesEndpoint,
		SourceResourceID: id,
		TargetResourceID: endpointID,
		TargetType:       aws.ResourceTypeRoute53ResolverEndpoint,
		Attributes: map[string]any{
			"rule_type": strings.TrimSpace(rule.RuleType),
		},
		SourceRecordID: id + "#endpoint#" + endpointID,
	}}
}

func ruleAssociationRelationships(
	boundary aws.Boundary,
	association ResolverRuleAssociation,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(association.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(association.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverRuleAssociationTargetsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if ruleID := strings.TrimSpace(association.ResolverRuleID); ruleID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverRuleAssociationUsesRule,
			SourceResourceID: id,
			TargetResourceID: ruleID,
			TargetType:       aws.ResourceTypeRoute53ResolverRule,
			SourceRecordID:   id + "#rule#" + ruleID,
		})
	}
	return observations
}

func firewallRuleGroupAssociationRelationships(
	boundary aws.Boundary,
	association FirewallRuleGroupAssociation,
) []aws.RelationshipObservation {
	id := strings.TrimSpace(association.ID)
	if id == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(association.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverFirewallRuleGroupAssociationTargetsVPC,
			SourceResourceID: id,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   id + "#vpc#" + vpcID,
		})
	}
	if groupID := strings.TrimSpace(association.FirewallRuleGroupID); groupID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipRoute53ResolverFirewallRuleGroupAssociationUsesRuleGroup,
			SourceResourceID: id,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeRoute53ResolverFirewallRuleGroup,
			SourceRecordID:   id + "#rule-group#" + groupID,
		})
	}
	return observations
}
