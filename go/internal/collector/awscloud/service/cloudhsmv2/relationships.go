// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// clusterVPCRelationship records the VPC that contains a CloudHSM v2 cluster.
// CloudHSM reports the bare VPC id (vpc-…), which is exactly the resource_id the
// EC2 scanner publishes for a VPC node, so the edge keys the bare id and never
// synthesizes an ARN. It returns nil when no VPC id is reported.
func clusterVPCRelationship(boundary awscloud.Boundary, cluster Cluster) *awscloud.RelationshipObservation {
	sourceID := clusterResourceID(cluster)
	vpcID := strings.TrimSpace(cluster.VPCID)
	if sourceID == "" || vpcID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudHSMV2ClusterInVPC,
		SourceResourceID: sourceID,
		TargetResourceID: vpcID,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipCloudHSMV2ClusterInVPC + ":" + vpcID,
	}
}

// clusterSecurityGroupRelationship records the AWS-managed cluster security
// group CloudHSM reports for a cluster. The bare security-group id (sg-…) is the
// resource_id the EC2 scanner publishes for a security-group node, so the edge
// keys the bare id. It returns nil when CloudHSM reports no security group.
func clusterSecurityGroupRelationship(boundary awscloud.Boundary, cluster Cluster) *awscloud.RelationshipObservation {
	sourceID := clusterResourceID(cluster)
	groupID := strings.TrimSpace(cluster.SecurityGroupID)
	if sourceID == "" || groupID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudHSMV2ClusterUsesSecurityGroup,
		SourceResourceID: sourceID,
		TargetResourceID: groupID,
		TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipCloudHSMV2ClusterUsesSecurityGroup + ":" + groupID,
	}
}

// clusterSubnetRelationships records one edge per availability-zone-to-subnet
// mapping CloudHSM reports for a cluster. Each subnet id is bare (subnet-…), the
// resource_id the EC2 scanner publishes for a subnet node, so every edge keys
// the bare id. Duplicate subnet ids across zones are de-duplicated so a cluster
// does not emit two identical edges. It returns nil when no subnet is reported.
func clusterSubnetRelationships(boundary awscloud.Boundary, cluster Cluster) []awscloud.RelationshipObservation {
	sourceID := clusterResourceID(cluster)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	seen := map[string]struct{}{}
	for _, mapping := range cluster.SubnetMappings {
		subnetID := strings.TrimSpace(mapping.SubnetID)
		if subnetID == "" {
			continue
		}
		if _, exists := seen[subnetID]; exists {
			continue
		}
		seen[subnetID] = struct{}{}
		attributes := map[string]any{}
		if zone := strings.TrimSpace(mapping.AvailabilityZone); zone != "" {
			attributes["availability_zone"] = zone
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudHSMV2ClusterInSubnet,
			SourceResourceID: sourceID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes:       attributes,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipCloudHSMV2ClusterInSubnet + ":" + subnetID,
		})
	}
	return relationships
}

// backupClusterRelationship records the source cluster a CloudHSM v2 backup was
// taken from. The target is the bare cluster id this scanner also publishes as a
// cluster node's resource_id, so the internal edge resolves once both the backup
// and its cluster are scanned. It returns nil when AWS reports no source cluster
// id (for example a backup whose cluster was deleted).
func backupClusterRelationship(boundary awscloud.Boundary, backup Backup) *awscloud.RelationshipObservation {
	sourceID := backupResourceID(backup)
	clusterID := strings.TrimSpace(backup.ClusterID)
	if sourceID == "" || clusterID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudHSMV2BackupOfCluster,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(backup.ARN),
		TargetResourceID: clusterID,
		TargetType:       awscloud.ResourceTypeCloudHSMV2Cluster,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipCloudHSMV2BackupOfCluster + ":" + clusterID,
	}
}
