// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesisanalyticsv2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// applicationRelationships builds every metadata-derived edge for one
// application: SQL input/output Kinesis data streams and Firehose delivery
// streams, the S3 code bucket, VPC subnets and security groups, the service
// execution IAM role, and CloudWatch logging log groups. Each helper keys the
// target by the exact resource_id the target scanner publishes so no edge
// dangles. Duplicate targets are emitted once.
func applicationRelationships(boundary awscloud.Boundary, application Application) []awscloud.RelationshipObservation {
	sourceID := applicationResourceID(application)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(application.ARN)
	var observations []awscloud.RelationshipObservation

	observations = appendStreamEdges(
		observations, boundary, sourceID, sourceARN,
		application.InputKinesisStreamARNs,
		awscloud.RelationshipManagedFlinkApplicationReadsFromKinesisStream,
		awscloud.ResourceTypeKinesisDataStream,
	)
	observations = appendStreamEdges(
		observations, boundary, sourceID, sourceARN,
		application.OutputKinesisStreamARNs,
		awscloud.RelationshipManagedFlinkApplicationWritesToKinesisStream,
		awscloud.ResourceTypeKinesisDataStream,
	)
	observations = appendStreamEdges(
		observations, boundary, sourceID, sourceARN,
		application.InputFirehoseStreamARNs,
		awscloud.RelationshipManagedFlinkApplicationReadsFromFirehoseStream,
		awscloud.ResourceTypeFirehoseDeliveryStream,
	)
	observations = appendStreamEdges(
		observations, boundary, sourceID, sourceARN,
		application.OutputFirehoseStreamARNs,
		awscloud.RelationshipManagedFlinkApplicationWritesToFirehoseStream,
		awscloud.ResourceTypeFirehoseDeliveryStream,
	)

	if edge, ok := codeBucketRelationship(boundary, sourceID, sourceARN, application); ok {
		observations = append(observations, edge)
	}
	observations = append(observations, networkRelationships(boundary, sourceID, sourceARN, application)...)
	if edge, ok := iamRoleRelationship(boundary, sourceID, sourceARN, application); ok {
		observations = append(observations, edge)
	}
	observations = appendLogGroupEdges(observations, boundary, sourceID, sourceARN, application.LogGroupARNs)
	return observations
}

// appendStreamEdges appends one edge per distinct stream ARN. AWS reports a full
// data stream / delivery stream ARN, which is the resource_id the kinesis and
// firehose scanners publish, so the edge keys the stream node by its ARN.
func appendStreamEdges(
	observations []awscloud.RelationshipObservation,
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	streamARNs []string,
	relationshipType, targetType string,
) []awscloud.RelationshipObservation {
	seen := make(map[string]struct{}, len(streamARNs))
	for _, streamARN := range streamARNs {
		streamARN = strings.TrimSpace(streamARN)
		if streamARN == "" {
			continue
		}
		if _, ok := seen[streamARN]; ok {
			continue
		}
		seen[streamARN] = struct{}{}
		targetARN := ""
		if isARN(streamARN) {
			targetARN = streamARN
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: relationshipType,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: streamARN,
			TargetARN:        targetARN,
			TargetType:       targetType,
			SourceRecordID:   sourceID + "->" + relationshipType + ":" + streamARN,
		})
	}
	return observations
}

// codeBucketRelationship records the application's S3 code-content bucket. AWS
// reports the bucket ARN, which is the resource_id the S3 scanner publishes, so
// the edge keys the bucket node by its ARN. Only the bucket identity and object
// key are recorded; the application code body is never read.
func codeBucketRelationship(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	application Application,
) (awscloud.RelationshipObservation, bool) {
	bucketARN := strings.TrimSpace(application.CodeS3BucketARN)
	if bucketARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetARN := ""
	if isARN(bucketARN) {
		targetARN = bucketARN
	}
	attributes := map[string]any{}
	if fileKey := strings.TrimSpace(application.CodeS3FileKey); fileKey != "" {
		attributes["object_key"] = fileKey
	}
	if len(attributes) == 0 {
		attributes = nil
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipManagedFlinkApplicationUsesS3CodeBucket,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: bucketARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipManagedFlinkApplicationUsesS3CodeBucket + ":" + bucketARN,
	}, true
}

// networkRelationships records the application's VPC subnet and security group
// placements. The EC2 scanner publishes subnets and security groups by their
// bare AWS id, so the edges key those nodes by the bare subnet-…/sg-… id. Each
// distinct id is emitted once across all VPC configurations.
func networkRelationships(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	application Application,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	seenSubnets := make(map[string]struct{})
	seenGroups := make(map[string]struct{})
	for _, config := range application.VPCConfigurations {
		vpcID := strings.TrimSpace(config.VPCID)
		for _, subnetID := range config.SubnetIDs {
			subnetID = strings.TrimSpace(subnetID)
			if subnetID == "" {
				continue
			}
			if _, ok := seenSubnets[subnetID]; ok {
				continue
			}
			seenSubnets[subnetID] = struct{}{}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipManagedFlinkApplicationUsesSubnet,
				SourceResourceID: sourceID,
				SourceARN:        sourceARN,
				TargetResourceID: subnetID,
				TargetType:       awscloud.ResourceTypeEC2Subnet,
				Attributes:       vpcAttribute(vpcID),
				SourceRecordID:   sourceID + "#subnet#" + subnetID,
			})
		}
		for _, groupID := range config.SecurityGroupIDs {
			groupID = strings.TrimSpace(groupID)
			if groupID == "" {
				continue
			}
			if _, ok := seenGroups[groupID]; ok {
				continue
			}
			seenGroups[groupID] = struct{}{}
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipManagedFlinkApplicationUsesSecurityGroup,
				SourceResourceID: sourceID,
				SourceARN:        sourceARN,
				TargetResourceID: groupID,
				TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
				Attributes:       vpcAttribute(vpcID),
				SourceRecordID:   sourceID + "#security-group#" + groupID,
			})
		}
	}
	return observations
}

// iamRoleRelationship records the application's service execution IAM role. AWS
// reports the role ARN, which is the resource_id the IAM scanner publishes for
// its roles, so the edge keys the role node by its ARN.
func iamRoleRelationship(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	application Application,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(application.ServiceExecutionRoleARN)
	if roleARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipManagedFlinkApplicationUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "#role#" + roleARN,
	}, true
}

// appendLogGroupEdges appends one edge per distinct CloudWatch log group ARN.
// The application's CloudWatch logging options report a log stream ARN; the
// client maps each to the non-wildcard log group ARN the cloudwatchlogs scanner
// publishes as its log group resource_id, so the edge keys that node by its ARN.
func appendLogGroupEdges(
	observations []awscloud.RelationshipObservation,
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	logGroupARNs []string,
) []awscloud.RelationshipObservation {
	seen := make(map[string]struct{}, len(logGroupARNs))
	for _, logGroupARN := range logGroupARNs {
		logGroupARN = strings.TrimSpace(logGroupARN)
		if logGroupARN == "" {
			continue
		}
		if _, ok := seen[logGroupARN]; ok {
			continue
		}
		seen[logGroupARN] = struct{}{}
		targetARN := ""
		if isARN(logGroupARN) {
			targetARN = logGroupARN
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipManagedFlinkApplicationLogsToCloudWatchLogGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: logGroupARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
			SourceRecordID:   sourceID + "#log-group#" + logGroupARN,
		})
	}
	return observations
}

// vpcAttribute returns the vpc_id attribute map for an edge, or nil when the VPC
// id is unknown so the payload omits an empty value.
func vpcAttribute(vpcID string) map[string]any {
	if vpcID == "" {
		return nil
	}
	return map[string]any{"vpc_id": vpcID}
}
