// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticache

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func clusterRelationships(boundary awscloud.Boundary, cluster CacheCluster) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	clusterARN := strings.TrimSpace(cluster.ARN)
	if vpcID := strings.TrimSpace(cluster.VPCID); vpcID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheClusterInVPC,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"vpc_id":                  vpcID,
				"cache_subnet_group_name": strings.TrimSpace(cluster.SubnetGroupName),
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipElastiCacheClusterInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(cluster.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheClusterInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"subnet_id":               subnetID,
				"cache_subnet_group_name": strings.TrimSpace(cluster.SubnetGroupName),
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipElastiCacheClusterInSubnet, subnetID),
		})
	}
	if kmsKey := strings.TrimSpace(cluster.KMSKeyID); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheClusterUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       "aws_kms_key",
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipElastiCacheClusterUsesKMSKey, kmsKey),
		})
	}
	return relationships
}

func replicationGroupRelationships(
	boundary awscloud.Boundary,
	group ReplicationGroup,
	clusterIdentities map[string]clusterIdentity,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(group.ARN, group.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	groupARN := strings.TrimSpace(group.ARN)
	for _, memberID := range cloneStrings(group.MemberClusters) {
		identity, ok := clusterIdentities[memberID]
		targetID := memberID
		targetARN := ""
		if ok && identity.arn != "" {
			targetID = identity.arn
			targetARN = identity.arn
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheReplicationGroupHasCluster,
			SourceResourceID: sourceID,
			SourceARN:        groupARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeElastiCacheCacheCluster,
			Attributes: map[string]any{
				"cache_cluster_id": memberID,
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipElastiCacheReplicationGroupHasCluster, targetID),
		})
	}
	return relationships
}

func userGroupRelationships(
	boundary awscloud.Boundary,
	group UserGroup,
	userIdentities map[string]userIdentity,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(group.ARN, group.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	groupARN := strings.TrimSpace(group.ARN)
	for _, userID := range cloneStrings(group.UserIDs) {
		identity, ok := userIdentities[userID]
		targetID := userID
		targetARN := ""
		if ok && identity.arn != "" {
			targetID = identity.arn
			targetARN = identity.arn
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheUserGroupHasUser,
			SourceResourceID: sourceID,
			SourceARN:        groupARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeElastiCacheUser,
			Attributes: map[string]any{
				"user_id": userID,
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipElastiCacheUserGroupHasUser, targetID),
		})
	}
	return relationships
}

type clusterIdentity struct {
	arn string
}

type userIdentity struct {
	arn string
}

func clusterIdentityMap(clusters []CacheCluster) map[string]clusterIdentity {
	identities := make(map[string]clusterIdentity, len(clusters))
	for _, cluster := range clusters {
		id := strings.TrimSpace(cluster.ID)
		if id == "" {
			continue
		}
		identities[id] = clusterIdentity{arn: strings.TrimSpace(cluster.ARN)}
	}
	return identities
}

func userIdentityMap(users []User) map[string]userIdentity {
	identities := make(map[string]userIdentity, len(users))
	for _, user := range users {
		id := strings.TrimSpace(user.ID)
		if id == "" {
			continue
		}
		identities[id] = userIdentity{arn: strings.TrimSpace(user.ARN)}
	}
	return identities
}

// relationshipRecordID encodes the relationship type into the durable
// SourceRecordID alongside the source and final target identity, matching the
// shape used by the RDS scanner. Including the relationship type keeps each
// relationship envelope's source ref distinct when a source has multiple edges
// to the same target and stays stable when the final target identity is
// upgraded from a raw cluster/user ID to the cluster/user ARN.
func relationshipRecordID(sourceID, relationshipType, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(relationshipType) + ":" + strings.TrimSpace(targetID)
}
