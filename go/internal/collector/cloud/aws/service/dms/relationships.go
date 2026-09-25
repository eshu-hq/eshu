// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dms

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// instanceRelationships records a replication instance's placement and
// dependency edges: its subnet group, the EC2 subnets and VPC reported on that
// subnet group, its VPC security groups, and its KMS encryption key. Each edge
// is keyed by the identity the target scanner publishes (bare AWS ids for EC2
// resources, the subnet-group identifier for the DMS subnet-group node, and the
// reported KMS key identifier for the KMS key node).
func instanceRelationships(
	boundary aws.Boundary,
	instance ReplicationInstance,
) []aws.RelationshipObservation {
	sourceID := instanceResourceID(instance)
	sourceARN := strings.TrimSpace(instance.ARN)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if subnetGroup := strings.TrimSpace(instance.SubnetGroupIdentifier); subnetGroup != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationInstanceInSubnetGroup,
			sourceID,
			sourceARN,
			subnetGroup,
			"",
			aws.ResourceTypeDMSReplicationSubnetGroup,
			nil,
		))
	}

	for _, subnetID := range cloneStrings(instance.SubnetIDs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationInstanceInSubnet,
			sourceID,
			sourceARN,
			subnetID,
			"",
			aws.ResourceTypeEC2Subnet,
			nil,
		))
	}

	for _, securityGroupID := range cloneStrings(instance.SecurityGroupIDs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationInstanceUsesSecurityGroup,
			sourceID,
			sourceARN,
			securityGroupID,
			"",
			aws.ResourceTypeEC2SecurityGroup,
			nil,
		))
	}

	if kmsKeyID := strings.TrimSpace(instance.KMSKeyID); kmsKeyID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationInstanceUsesKMSKey,
			sourceID,
			sourceARN,
			kmsKeyID,
			arnIfARN(kmsKeyID),
			aws.ResourceTypeKMSKey,
			nil,
		))
	}

	return relationships
}

// subnetGroupRelationships records a replication subnet group's VPC placement
// and its member subnets. The VPC and subnets are keyed by the bare AWS ids the
// EC2 scanner publishes.
func subnetGroupRelationships(
	boundary aws.Boundary,
	group ReplicationSubnetGroup,
) []aws.RelationshipObservation {
	sourceID := subnetGroupResourceID(group)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(group.VPCID); vpcID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationSubnetGroupInVPC,
			sourceID,
			"",
			vpcID,
			"",
			aws.ResourceTypeEC2VPC,
			nil,
		))
	}

	for _, subnetID := range cloneStrings(group.SubnetIDs) {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationSubnetGroupHasSubnet,
			sourceID,
			"",
			subnetID,
			"",
			aws.ResourceTypeEC2Subnet,
			nil,
		))
	}

	return relationships
}

// endpointRelationships records a DMS endpoint's resolvable data-store and
// dependency edges: its KMS encryption key, an S3 target bucket (keyed by the
// synthesized partition-aware bucket ARN the S3 scanner publishes), a Kinesis
// target stream (keyed by the stream ARN DMS reports), and a Secrets Manager
// secret reference. Edges are emitted only when DMS reports a resolvable target
// identity, so an endpoint to an unscanned data store never dangles.
func endpointRelationships(
	boundary aws.Boundary,
	endpoint Endpoint,
) []aws.RelationshipObservation {
	sourceID := endpointResourceID(endpoint)
	sourceARN := strings.TrimSpace(endpoint.ARN)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if kmsKeyID := strings.TrimSpace(endpoint.KMSKeyID); kmsKeyID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSEndpointUsesKMSKey,
			sourceID,
			sourceARN,
			kmsKeyID,
			arnIfARN(kmsKeyID),
			aws.ResourceTypeKMSKey,
			nil,
		))
	}

	if bucket := strings.TrimSpace(endpoint.S3BucketName); bucket != "" {
		bucketARN := arnForBucket(aws.PartitionForBoundary(boundary), bucket)
		if bucketARN != "" {
			relationships = append(relationships, relationship(
				boundary,
				aws.RelationshipDMSEndpointTargetsS3Bucket,
				sourceID,
				sourceARN,
				bucketARN,
				bucketARN,
				aws.ResourceTypeS3Bucket,
				map[string]any{"bucket": bucket},
			))
		}
	}

	if streamARN := strings.TrimSpace(endpoint.KinesisStreamARN); streamARN != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSEndpointTargetsKinesisStream,
			sourceID,
			sourceARN,
			streamARN,
			arnIfARN(streamARN),
			aws.ResourceTypeKinesisDataStream,
			nil,
		))
	}

	if secretID := strings.TrimSpace(endpoint.SecretsManagerSecretID); secretID != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSEndpointUsesSecret,
			sourceID,
			sourceARN,
			secretID,
			arnIfARN(secretID),
			aws.ResourceTypeSecretsManagerSecret,
			nil,
		))
	}

	return relationships
}

// taskRelationships records a replication task's source endpoint, target
// endpoint, and replication instance. Each edge is keyed by the ARN the target
// node publishes (the endpoint ARN for endpoints, the instance ARN for the
// replication instance). Edges are emitted only when the task reports the
// target ARN, so a task never dangles to an unreported endpoint or instance.
func taskRelationships(
	boundary aws.Boundary,
	task ReplicationTask,
) []aws.RelationshipObservation {
	sourceID := taskResourceID(task)
	sourceARN := strings.TrimSpace(task.ARN)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if sourceEndpointARN := strings.TrimSpace(task.SourceEndpointARN); sourceEndpointARN != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationTaskUsesSourceEndpoint,
			sourceID,
			sourceARN,
			sourceEndpointARN,
			arnIfARN(sourceEndpointARN),
			aws.ResourceTypeDMSEndpoint,
			nil,
		))
	}

	if targetEndpointARN := strings.TrimSpace(task.TargetEndpointARN); targetEndpointARN != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationTaskUsesTargetEndpoint,
			sourceID,
			sourceARN,
			targetEndpointARN,
			arnIfARN(targetEndpointARN),
			aws.ResourceTypeDMSEndpoint,
			nil,
		))
	}

	if instanceARN := strings.TrimSpace(task.ReplicationInstanceARN); instanceARN != "" {
		relationships = append(relationships, relationship(
			boundary,
			aws.RelationshipDMSReplicationTaskRunsOnInstance,
			sourceID,
			sourceARN,
			instanceARN,
			arnIfARN(instanceARN),
			aws.ResourceTypeDMSReplicationInstance,
			nil,
		))
	}

	return relationships
}

// relationship builds a relationship observation with a deterministic
// SourceRecordID so repeated observations of the same edge in one AWS
// generation coalesce.
func relationship(
	boundary aws.Boundary,
	relationshipType string,
	sourceID string,
	sourceARN string,
	targetID string,
	targetARN string,
	targetType string,
	attributes map[string]any,
) aws.RelationshipObservation {
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + targetID,
	}
}
