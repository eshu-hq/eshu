// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func subnetVPCRelationship(boundary aws.Boundary, subnet Subnet) (aws.RelationshipObservation, bool) {
	subnetID := strings.TrimSpace(subnet.ID)
	vpcID := strings.TrimSpace(subnet.VPCID)
	if subnetID == "" || vpcID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEC2SubnetInVPC,
		SourceResourceID: subnetID,
		SourceARN:        strings.TrimSpace(subnet.ARN),
		TargetResourceID: vpcID,
		TargetType:       aws.ResourceTypeEC2VPC,
		SourceRecordID:   subnetID + "#vpc#" + vpcID,
	}, true
}

func securityGroupVPCRelationship(
	boundary aws.Boundary,
	group SecurityGroup,
) (aws.RelationshipObservation, bool) {
	groupID := strings.TrimSpace(group.ID)
	vpcID := strings.TrimSpace(group.VPCID)
	if groupID == "" || vpcID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEC2SecurityGroupInVPC,
		SourceResourceID: groupID,
		TargetResourceID: vpcID,
		TargetType:       aws.ResourceTypeEC2VPC,
		SourceRecordID:   groupID + "#vpc#" + vpcID,
	}, true
}

func securityGroupRuleRelationship(
	boundary aws.Boundary,
	rule SecurityGroupRule,
) (aws.RelationshipObservation, bool) {
	ruleID := securityGroupRuleID(rule)
	groupID := strings.TrimSpace(rule.GroupID)
	if ruleID == "" || groupID == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEC2SecurityGroupHasRule,
		SourceResourceID: groupID,
		TargetResourceID: ruleID,
		TargetType:       aws.ResourceTypeEC2SecurityGroupRule,
		Attributes: map[string]any{
			"direction": securityGroupRuleDirection(rule),
		},
		SourceRecordID: groupID + "#rule#" + ruleID,
	}, true
}

func networkInterfaceRelationships(
	boundary aws.Boundary,
	networkInterface NetworkInterface,
) []aws.RelationshipObservation {
	networkInterfaceID := strings.TrimSpace(networkInterface.ID)
	if networkInterfaceID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if subnetID := strings.TrimSpace(networkInterface.SubnetID); subnetID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEC2NetworkInterfaceInSubnet,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   networkInterfaceID + "#subnet#" + subnetID,
		})
	}
	if vpcID := strings.TrimSpace(networkInterface.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEC2NetworkInterfaceInVPC,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   networkInterfaceID + "#vpc#" + vpcID,
		})
	}
	for _, group := range networkInterface.SecurityGroups {
		groupID := strings.TrimSpace(group.ID)
		if groupID == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipEC2NetworkInterfaceUsesSecurityGroup,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			Attributes: map[string]any{
				"group_name": strings.TrimSpace(group.Name),
			},
			SourceRecordID: networkInterfaceID + "#security-group#" + groupID,
		})
	}
	if attachment := attachmentRelationship(boundary, networkInterface); attachment.RelationshipType != "" {
		observations = append(observations, attachment)
	}
	return observations
}

func attachmentRelationship(
	boundary aws.Boundary,
	networkInterface NetworkInterface,
) aws.RelationshipObservation {
	if networkInterface.Attachment == nil {
		return aws.RelationshipObservation{}
	}
	networkInterfaceID := strings.TrimSpace(networkInterface.ID)
	targetARN := strings.TrimSpace(networkInterface.Attachment.AttachedResourceARN)
	targetID := firstNonEmpty(strings.TrimSpace(networkInterface.Attachment.InstanceID), targetARN)
	if networkInterfaceID == "" || targetID == "" {
		return aws.RelationshipObservation{}
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipEC2NetworkInterfaceAttachedToResource,
		SourceResourceID: networkInterfaceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       strings.TrimSpace(networkInterface.Attachment.AttachedResourceType),
		Attributes:       attachmentMap(networkInterface.Attachment),
		SourceRecordID:   networkInterfaceID + "#attachment#" + targetID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
