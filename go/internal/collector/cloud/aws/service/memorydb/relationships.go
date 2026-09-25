// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package memorydb

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func clusterRelationships(
	boundary aws.Boundary,
	cluster Cluster,
	subnetGroupIdentities map[string]subnetGroupIdentity,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(cluster.ARN, cluster.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	clusterARN := strings.TrimSpace(cluster.ARN)
	if subnetGroupName := strings.TrimSpace(cluster.SubnetGroupName); subnetGroupName != "" {
		identity, ok := subnetGroupIdentities[subnetGroupName]
		targetID := subnetGroupName
		targetARN := ""
		if ok && identity.arn != "" {
			targetID = identity.arn
			targetARN = identity.arn
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMemoryDBClusterInSubnetGroup,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeMemoryDBSubnetGroup,
			Attributes: map[string]any{
				"subnet_group_name": subnetGroupName,
			},
			SourceRecordID: relationshipRecordID(sourceID, aws.RelationshipMemoryDBClusterInSubnetGroup, targetID),
		})
	}
	if kmsKey := strings.TrimSpace(cluster.KMSKeyID); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMemoryDBClusterUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       "aws_kms_key",
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipMemoryDBClusterUsesKMSKey, kmsKey),
		})
	}
	if topicARN := strings.TrimSpace(cluster.SNSTopicARN); topicARN != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMemoryDBClusterNotifiesSNSTopic,
			SourceResourceID: sourceID,
			SourceARN:        clusterARN,
			TargetResourceID: topicARN,
			TargetARN:        topicARN,
			TargetType:       aws.ResourceTypeSNSTopic,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipMemoryDBClusterNotifiesSNSTopic, topicARN),
		})
	}
	return relationships
}

func aclRelationships(
	boundary aws.Boundary,
	acl ACL,
	userIdentities map[string]userIdentity,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(acl.ARN, acl.Name)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	aclARN := strings.TrimSpace(acl.ARN)
	for _, userName := range cloneStrings(acl.UserNames) {
		identity, ok := userIdentities[userName]
		targetID := userName
		targetARN := ""
		if ok && identity.arn != "" {
			targetID = identity.arn
			targetARN = identity.arn
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMemoryDBACLHasUser,
			SourceResourceID: sourceID,
			SourceARN:        aclARN,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeMemoryDBUser,
			Attributes: map[string]any{
				"user_name": userName,
			},
			SourceRecordID: relationshipRecordID(sourceID, aws.RelationshipMemoryDBACLHasUser, targetID),
		})
	}
	return relationships
}

type subnetGroupIdentity struct {
	arn string
}

type userIdentity struct {
	arn string
}

func subnetGroupIdentityMap(groups []SubnetGroup) map[string]subnetGroupIdentity {
	identities := make(map[string]subnetGroupIdentity, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		identities[name] = subnetGroupIdentity{arn: strings.TrimSpace(group.ARN)}
	}
	return identities
}

func userIdentityMap(users []User) map[string]userIdentity {
	identities := make(map[string]userIdentity, len(users))
	for _, user := range users {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			continue
		}
		identities[name] = userIdentity{arn: strings.TrimSpace(user.ARN)}
	}
	return identities
}

// relationshipRecordID encodes the relationship type into the durable
// SourceRecordID alongside the source and final target identity, matching the
// shape used by the ElastiCache scanner. Including the relationship type keeps
// each relationship envelope's source ref distinct when a source has multiple
// edges to the same target and stays stable when the final target identity is
// upgraded from a raw subnet-group/user name to the subnet-group/user ARN.
func relationshipRecordID(sourceID, relationshipType, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(relationshipType) + ":" + strings.TrimSpace(targetID)
}
