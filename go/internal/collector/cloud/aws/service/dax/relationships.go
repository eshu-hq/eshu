// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// clusterRelationships emits the direct dependency edges a DAX cluster reports:
// its subnet group placement, each attached VPC security group, and the IAM
// role it assumes to reach DynamoDB. Each edge is emitted only when AWS reports
// the target identity, so empty accounts and partial responses produce no
// dangling edges. The subnet/VPC edges are not emitted here; they belong to the
// subnet group resource, which owns the authoritative VPC and member-subnet ids.
func clusterRelationships(boundary aws.Boundary, cluster Cluster) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.Name)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(cluster.ARN)
	var relationships []aws.RelationshipObservation

	if subnetGroupName := strings.TrimSpace(cluster.SubnetGroupName); subnetGroupName != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDAXClusterInSubnetGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetGroupName,
			TargetType:       aws.ResourceTypeDAXSubnetGroup,
			Attributes: map[string]any{
				"subnet_group_name": subnetGroupName,
			},
			SourceRecordID: relationshipRecordID(sourceID, aws.RelationshipDAXClusterInSubnetGroup, subnetGroupName),
		})
	}

	for _, securityGroupID := range cloneStrings(cluster.SecurityGroupIDs) {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDAXClusterUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: securityGroupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDAXClusterUsesSecurityGroup, securityGroupID),
		})
	}

	if roleARN := strings.TrimSpace(cluster.IAMRoleARN); roleARN != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDAXClusterAssumesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDAXClusterAssumesIAMRole, roleARN),
		})
	}

	return relationships
}

// subnetGroupRelationships emits the VPC placement edge and one membership edge
// per subnet a DAX subnet group reports. The subnet group is keyed by name (DAX
// subnet groups have no ARN); the VPC and subnet targets are bare AWS ids, which
// is how the EC2 scanner publishes those resource_ids. Edges are emitted only
// when the target id is present.
func subnetGroupRelationships(boundary aws.Boundary, group SubnetGroup) []aws.RelationshipObservation {
	sourceID := strings.TrimSpace(group.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(group.VPCID); vpcID != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDAXSubnetGroupInVPC,
			SourceResourceID: sourceID,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDAXSubnetGroupInVPC, vpcID),
		})
	}

	for _, subnetID := range cloneStrings(group.SubnetIDs) {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDAXSubnetGroupHasSubnet,
			SourceResourceID: sourceID,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDAXSubnetGroupHasSubnet, subnetID),
		})
	}

	return relationships
}
