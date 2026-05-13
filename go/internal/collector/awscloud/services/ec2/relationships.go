package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func subnetVPCRelationship(boundary awscloud.Boundary, subnet Subnet) (awscloud.RelationshipObservation, bool) {
	subnetID := strings.TrimSpace(subnet.ID)
	vpcID := strings.TrimSpace(subnet.VPCID)
	if subnetID == "" || vpcID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2SubnetInVPC,
		SourceResourceID: subnetID,
		SourceARN:        strings.TrimSpace(subnet.ARN),
		TargetResourceID: vpcID,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		SourceRecordID:   subnetID + "#vpc#" + vpcID,
	}, true
}

func securityGroupVPCRelationship(
	boundary awscloud.Boundary,
	group SecurityGroup,
) (awscloud.RelationshipObservation, bool) {
	groupID := strings.TrimSpace(group.ID)
	vpcID := strings.TrimSpace(group.VPCID)
	if groupID == "" || vpcID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2SecurityGroupInVPC,
		SourceResourceID: groupID,
		TargetResourceID: vpcID,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		SourceRecordID:   groupID + "#vpc#" + vpcID,
	}, true
}

func securityGroupRuleRelationship(
	boundary awscloud.Boundary,
	rule SecurityGroupRule,
) (awscloud.RelationshipObservation, bool) {
	ruleID := securityGroupRuleID(rule)
	groupID := strings.TrimSpace(rule.GroupID)
	if ruleID == "" || groupID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2SecurityGroupHasRule,
		SourceResourceID: groupID,
		TargetResourceID: ruleID,
		TargetType:       awscloud.ResourceTypeEC2SecurityGroupRule,
		Attributes: map[string]any{
			"direction": securityGroupRuleDirection(rule),
		},
		SourceRecordID: groupID + "#rule#" + ruleID,
	}, true
}

func networkInterfaceRelationships(
	boundary awscloud.Boundary,
	networkInterface NetworkInterface,
) []awscloud.RelationshipObservation {
	networkInterfaceID := strings.TrimSpace(networkInterface.ID)
	if networkInterfaceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if subnetID := strings.TrimSpace(networkInterface.SubnetID); subnetID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipEC2NetworkInterfaceInSubnet,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   networkInterfaceID + "#subnet#" + subnetID,
		})
	}
	if vpcID := strings.TrimSpace(networkInterface.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipEC2NetworkInterfaceInVPC,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   networkInterfaceID + "#vpc#" + vpcID,
		})
	}
	for _, group := range networkInterface.SecurityGroups {
		groupID := strings.TrimSpace(group.ID)
		if groupID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipEC2NetworkInterfaceUsesSecurityGroup,
			SourceResourceID: networkInterfaceID,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
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
	boundary awscloud.Boundary,
	networkInterface NetworkInterface,
) awscloud.RelationshipObservation {
	if networkInterface.Attachment == nil {
		return awscloud.RelationshipObservation{}
	}
	networkInterfaceID := strings.TrimSpace(networkInterface.ID)
	targetARN := strings.TrimSpace(networkInterface.Attachment.AttachedResourceARN)
	targetID := firstNonEmpty(targetARN, strings.TrimSpace(networkInterface.Attachment.InstanceID))
	if networkInterfaceID == "" || targetID == "" {
		return awscloud.RelationshipObservation{}
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2NetworkInterfaceAttachedToResource,
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
