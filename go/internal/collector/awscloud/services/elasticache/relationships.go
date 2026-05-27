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
		vpcARN := vpcARNFor(boundary, vpcID)
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheClusterInVPC,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: vpcARN,
			TargetARN:        vpcARN,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes: map[string]any{
				"vpc_id":                 vpcID,
				"cache_subnet_group_name": strings.TrimSpace(cluster.SubnetGroupName),
			},
			SourceRecordID: sourceID + "->" + vpcARN,
		})
	}
	for _, subnetID := range cloneStrings(cluster.SubnetIDs) {
		subnetARN := subnetARNFor(boundary, subnetID)
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElastiCacheClusterInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: subnetARN,
			TargetARN:        subnetARN,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes: map[string]any{
				"subnet_id":               subnetID,
				"cache_subnet_group_name": strings.TrimSpace(cluster.SubnetGroupName),
			},
			SourceRecordID: sourceID + "->" + subnetARN,
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
			SourceRecordID:   sourceID + "->" + kmsKey,
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
		if ok {
			if identity.arn != "" {
				targetID = identity.arn
				targetARN = identity.arn
			}
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
			SourceRecordID: sourceID + "->" + memberID,
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
			SourceRecordID: sourceID + "->" + userID,
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

func vpcARNFor(boundary awscloud.Boundary, vpcID string) string {
	vpcID = strings.TrimSpace(vpcID)
	if vpcID == "" {
		return ""
	}
	if isARN(vpcID) {
		return vpcID
	}
	return "arn:aws:ec2:" + boundary.Region + ":" + boundary.AccountID + ":vpc/" + vpcID
}

func subnetARNFor(boundary awscloud.Boundary, subnetID string) string {
	subnetID = strings.TrimSpace(subnetID)
	if subnetID == "" {
		return ""
	}
	if isARN(subnetID) {
		return subnetID
	}
	return "arn:aws:ec2:" + boundary.Region + ":" + boundary.AccountID + ":subnet/" + subnetID
}
