// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// canaryRelationships returns the reported dependency edges for one canary: the
// S3 artifact bucket (partition-aware bucket ARN, matching the S3 scanner), the
// execution IAM role (ARN, matching the IAM scanner), and, when the canary runs
// in a VPC, its subnets and security groups (bare ids, matching the EC2
// scanner). Each edge is emitted only when its endpoint identity resolves, so no
// edge dangles. It returns nil when the canary has no resolvable identity.
func canaryRelationships(boundary awscloud.Boundary, canary Canary) []awscloud.RelationshipObservation {
	canaryID := canaryResourceID(canary)
	if canaryID == "" {
		return nil
	}
	canaryARN := strings.TrimSpace(canary.ARN)
	var observations []awscloud.RelationshipObservation

	if bucket := bucketNameFromArtifactLocation(canary.ArtifactS3Location); bucket != "" {
		if bucketARN := arnForBucket(awscloud.PartitionForBoundary(boundary), bucket); bucketARN != "" {
			attributes := map[string]any{"bucket": bucket}
			if mode := strings.TrimSpace(canary.ArtifactEncryptionMode); mode != "" {
				attributes["encryption_mode"] = mode
			}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipSyntheticsCanaryUsesS3Bucket,
				SourceResourceID: canaryID,
				SourceARN:        canaryARN,
				TargetResourceID: bucketARN,
				TargetARN:        bucketARN,
				TargetType:       awscloud.ResourceTypeS3Bucket,
				Attributes:       attributes,
				SourceRecordID:   canaryID + "#s3#" + bucketARN,
			})
		}
	}

	if roleARN := strings.TrimSpace(canary.ExecutionRoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipSyntheticsCanaryUsesIAMRole,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   canaryID + "#role#" + roleARN,
		})
	}

	for _, subnetID := range cloneStrings(canary.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipSyntheticsCanaryUsesSubnet,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   canaryID + "#subnet#" + subnetID,
		})
	}

	for _, groupID := range cloneStrings(canary.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipSyntheticsCanaryUsesSecurityGroup,
			SourceResourceID: canaryID,
			SourceARN:        canaryARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   canaryID + "#security-group#" + groupID,
		})
	}

	return observations
}
