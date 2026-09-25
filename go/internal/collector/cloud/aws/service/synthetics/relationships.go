// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// canaryRelationships returns the reported dependency edges for one canary: the
// S3 artifact bucket (partition-aware bucket ARN, matching the S3 scanner), the
// execution IAM role (ARN, matching the IAM scanner), and, when the canary runs
// in a VPC, its subnets and security groups (bare ids, matching the EC2
// scanner). Each edge is emitted only when its endpoint identity resolves, so no
// edge dangles. It returns nil when the canary has no resolvable identity.
func canaryRelationships(boundary aws.Boundary, canary Canary) []aws.RelationshipObservation {
	canaryID := canaryResourceID(canary)
	if canaryID == "" {
		return nil
	}
	canaryARN := strings.TrimSpace(canary.ARN)
	var observations []aws.RelationshipObservation

	if bucket := bucketNameFromArtifactLocation(canary.ArtifactS3Location); bucket != "" {
		if bucketARN := arnForBucket(aws.PartitionForBoundary(boundary), bucket); bucketARN != "" {
			attributes := map[string]any{"bucket": bucket}
			if mode := strings.TrimSpace(canary.ArtifactEncryptionMode); mode != "" {
				attributes["encryption_mode"] = mode
			}
			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipSyntheticsCanaryUsesS3Bucket,
				SourceResourceID: canaryID,
				SourceARN:        canaryARN,
				TargetResourceID: bucketARN,
				TargetARN:        bucketARN,
				TargetType:       aws.ResourceTypeS3Bucket,
				Attributes:       attributes,
				SourceRecordID:   canaryID + "#s3#" + bucketARN,
			})
		}
	}

	if roleARN := strings.TrimSpace(canary.ExecutionRoleARN); roleARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipSyntheticsCanaryUsesIAMRole,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   canaryID + "#role#" + roleARN,
		})
	}

	for _, subnetID := range cloneStrings(canary.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipSyntheticsCanaryUsesSubnet,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   canaryID + "#subnet#" + subnetID,
		})
	}

	for _, groupID := range cloneStrings(canary.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipSyntheticsCanaryUsesSecurityGroup,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   canaryID + "#security-group#" + groupID,
		})
	}

	return observations
}
