// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// environmentRelationships returns every outgoing edge for one MWAA
// environment. Each edge is sourced on the same identifier the environment
// resource publishes as its resource_id (the environment ARN, falling back to
// the name) so the source node and the edge source agree. An edge is emitted
// only when AWS reports a non-empty, well-shaped target identifier that matches
// how the target scanner publishes its resource_id, otherwise the edge is
// skipped rather than dangled.
func environmentRelationships(boundary awscloud.Boundary, environment Environment) []awscloud.RelationshipObservation {
	sourceID := environmentResourceID(environment)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(environment.ARN)
	var observations []awscloud.RelationshipObservation

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
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (awscloud.RelationshipObservation, bool) {
	bucketARN := s3BucketARN(boundary, environment.SourceBucketARN)
	if bucketARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMWAAEnvironmentUsesS3Bucket,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentUsesS3Bucket + ":" + bucketARN,
	}, true
}

func environmentIAMRoleRelationship(
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(environment.ExecutionRoleARN)
	if !isARN(roleARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMWAAEnvironmentUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentUsesIAMRole + ":" + roleARN,
	}, true
}

func environmentKMSKeyRelationship(
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) (awscloud.RelationshipObservation, bool) {
	kmsKey := strings.TrimSpace(environment.KMSKey)
	if kmsKey == "" {
		return awscloud.RelationshipObservation{}, false
	}
	// The kms scanner publishes resource_id as the bare key id when present and
	// the key ARN otherwise. MWAA reports an ARN, so target both the resource_id
	// and target_arn with the ARN; the kms scanner also carries the ARN as a
	// correlation anchor, so the edge still joins.
	targetARN := ""
	if isARN(kmsKey) {
		targetARN = kmsKey
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipMWAAEnvironmentUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: kmsKey,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentUsesKMSKey + ":" + kmsKey,
	}, true
}

func environmentSubnetRelationships(
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []awscloud.RelationshipObservation {
	observations := make([]awscloud.RelationshipObservation, 0, len(environment.SubnetIDs))
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMWAAEnvironmentUsesSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentUsesSubnet + ":" + subnetID,
		})
	}
	return observations
}

func environmentSecurityGroupRelationships(
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []awscloud.RelationshipObservation {
	observations := make([]awscloud.RelationshipObservation, 0, len(environment.SecurityGroupIDs))
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMWAAEnvironmentUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentUsesSecurityGroup + ":" + groupID,
		})
	}
	return observations
}

func environmentLogGroupRelationships(
	boundary awscloud.Boundary,
	environment Environment,
	sourceID string,
	sourceARN string,
) []awscloud.RelationshipObservation {
	observations := make([]awscloud.RelationshipObservation, 0, len(environment.LogGroups))
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
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: logGroupARN,
			TargetARN:        logGroupARN,
			TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
			Attributes:       attributes,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup + ":" + logGroupARN,
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
