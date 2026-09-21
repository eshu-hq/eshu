// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// environmentRelationships returns every outgoing edge for one MWAA
// environment. Each edge is sourced on the same identifier the environment
// resource publishes as its resource_id (the environment ARN, falling back to
// the name) so the source node and the edge source agree. An edge is emitted
// only when AWS reports a non-empty, well-shaped target identifier that matches
// how the target scanner publishes its resource_id, otherwise the edge is
// skipped rather than dangled.
func environmentRelationships(boundary aws.Boundary, environment Environment) []aws.RelationshipObservation {
	sourceID := environmentResourceID(environment)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(environment.ARN)
	var observations []aws.RelationshipObservation

	if rel, ok := environmentS3Relationship(boundary, environment, sourceID, sourceARN); ok {
		observations = append(observations, rel)
	}
	if rel, ok := environmentIAMRoleRelationship(boundary, environment, sourceID, sourceARN); ok {
		observations = append(observations, rel)
	}
	if rel, ok := environmentKMSKeyRelationship(boundary, environment, sourceID, sourceARN); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, environmentSubnetRelationships(boundary, environment, sourceID, sourceARN)...)
	observations = append(observations, environmentSecurityGroupRelationships(boundary, environment, sourceID, sourceARN)...)
	observations = append(observations, environmentLogGroupRelationships(boundary, environment, sourceID, sourceARN)...)

	return observations
}

func environmentS3Relationship(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (aws.RelationshipObservation, bool) {
	bucketARN := s3BucketARN(boundary, environment.SourceBucketARN)
	if bucketARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipMWAAEnvironmentUsesS3Bucket,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentUsesS3Bucket + ":" + bucketARN,
	}, true
}

func environmentIAMRoleRelationship(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (aws.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(environment.ExecutionRoleARN)
	if !isARN(roleARN) {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipMWAAEnvironmentUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentUsesIAMRole + ":" + roleARN,
	}, true
}

func environmentKMSKeyRelationship(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (aws.RelationshipObservation, bool) {
	kmsKey := strings.TrimSpace(environment.KMSKey)
	if kmsKey == "" {
		return aws.RelationshipObservation{}, false
	}
	// The kms scanner publishes resource_id as the bare key id when present and
	// the key ARN otherwise. MWAA reports an ARN, so target both the resource_id
	// and target_arn with the ARN; the kms scanner also carries the ARN as a
	// correlation anchor, so the edge still joins.
	targetARN := ""
	if isARN(kmsKey) {
		targetARN = kmsKey
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipMWAAEnvironmentUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: kmsKey,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentUsesKMSKey + ":" + kmsKey,
	}, true
}

func environmentSubnetRelationships(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []aws.RelationshipObservation {
	observations := make([]aws.RelationshipObservation, 0, len(environment.SubnetIDs))
	seen := make(map[string]struct{}, len(environment.SubnetIDs))
	for _, subnetID := range environment.SubnetIDs {
		subnetID = strings.TrimSpace(subnetID)
		if subnetID == "" {
			continue
		}
		if _, ok := seen[subnetID]; ok {
			continue
		}
		seen[subnetID] = struct{}{}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMWAAEnvironmentUsesSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentUsesSubnet + ":" + subnetID,
		})
	}
	return observations
}

func environmentSecurityGroupRelationships(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []aws.RelationshipObservation {
	observations := make([]aws.RelationshipObservation, 0, len(environment.SecurityGroupIDs))
	seen := make(map[string]struct{}, len(environment.SecurityGroupIDs))
	for _, groupID := range environment.SecurityGroupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMWAAEnvironmentUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentUsesSecurityGroup + ":" + groupID,
		})
	}
	return observations
}

func environmentLogGroupRelationships(
	boundary aws.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []aws.RelationshipObservation {
	observations := make([]aws.RelationshipObservation, 0, len(environment.LogGroups))
	seen := make(map[string]struct{}, len(environment.LogGroups))
	for _, logGroup := range environment.LogGroups {
		// AWS reports a log group ARN even for disabled modules. A disabled
		// module does not publish Airflow logs, so emitting an edge would create
		// misleading dependency evidence; skip it.
		if !logGroup.Enabled {
			continue
		}
		logGroupARN := trimLogGroupWildcardARN(logGroup.ARN)
		if logGroupARN == "" {
			continue
		}
		if _, ok := seen[logGroupARN]; ok {
			continue
		}
		seen[logGroupARN] = struct{}{}
		attributes := map[string]any{
			"log_module": strings.TrimSpace(logGroup.Module),
			"enabled":    logGroup.Enabled,
		}
		if level := strings.TrimSpace(logGroup.LogLevel); level != "" {
			attributes["log_level"] = level
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: logGroupARN,
			TargetARN:        logGroupARN,
			TargetType:       aws.ResourceTypeCloudWatchLogsLogGroup,
			Attributes:       attributes,
			SourceRecordID:   sourceID + "->" + aws.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup + ":" + logGroupARN,
		})
	}
	return observations
}

// environmentResourceID returns the identifier the environment resource
// publishes as its resource_id: the environment ARN when present, otherwise the
// environment name. Every outgoing edge is sourced on this same value.
func environmentResourceID(environment Environment) string {
	if arn := strings.TrimSpace(environment.ARN); arn != "" {
		return arn
	}
	return strings.TrimSpace(environment.Name)
}
