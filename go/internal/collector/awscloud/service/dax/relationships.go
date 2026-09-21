// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dax

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// clusterRelationships emits the direct dependency edges a DAX cluster reports:
// its subnet group placement, each attached VPC security group, and the IAM
// role it assumes to reach DynamoDB. Each edge is emitted only when AWS reports
// the target identity, so empty accounts and partial responses produce no
// dangling edges. The subnet/VPC edges are not emitted here; they belong to the
// subnet group resource, which owns the authoritative VPC and member-subnet ids.
func clusterRelationships(boundary awscloud.Boundary, cluster Cluster) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.Name)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(cluster.ARN)
	var relationships []awscloud.RelationshipObservation

	if subnetGroupName := strings.TrimSpace(cluster.SubnetGroupName); subnetGroupName != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDAXClusterInSubnetGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetGroupName,
			TargetType:       awscloud.ResourceTypeDAXSubnetGroup,
			Attributes: map[string]any{
				"subnet_group_name": subnetGroupName,
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipDAXClusterInSubnetGroup, subnetGroupName),
		})
	}

	for _, securityGroupID := range cloneStrings(cluster.SecurityGroupIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDAXClusterUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: securityGroupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipDAXClusterUsesSecurityGroup, securityGroupID),
		})
	}

	if roleARN := strings.TrimSpace(cluster.IAMRoleARN); roleARN != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDAXClusterAssumesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipDAXClusterAssumesIAMRole, roleARN),
		})
	}

	return relationships
}

// subnetGroupRelationships emits the VPC placement edge and one membership edge
// per subnet a DAX subnet group reports. The subnet group is keyed by name (DAX
// subnet groups have no ARN); the VPC and subnet targets are bare AWS ids, which
// is how the EC2 scanner publishes those resource_ids. Edges are emitted only
// when the target id is present.
func subnetGroupRelationships(boundary awscloud.Boundary, group SubnetGroup) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(group.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation

	if vpcID := strings.TrimSpace(group.VPCID); vpcID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDAXSubnetGroupInVPC,
			SourceResourceID: sourceID,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipDAXSubnetGroupInVPC, vpcID),
		})
	}

	for _, subnetID := range cloneStrings(group.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDAXSubnetGroupHasSubnet,
			SourceResourceID: sourceID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipDAXSubnetGroupHasSubnet, subnetID),
		})
	}

	return relationships
}
