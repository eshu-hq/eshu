// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fis

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// templateRelationships builds every resolvable relationship an experiment
// template reports: the execution IAM role, the explicit resource targets
// (EC2 instance, ECS cluster, RDS DB instance/cluster), the logging
// destinations (CloudWatch log group, S3 bucket), and the CloudWatch alarm
// stop conditions. Unresolvable references (a non-ARN role, an unrecognized
// target ARN family, a missing log group) are skipped rather than keyed to a
// dangling target.
func templateRelationships(boundary awscloud.Boundary, template ExperimentTemplate) []awscloud.RelationshipObservation {
	sourceID := templateResourceID(template)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	if rel := templateIAMRoleRelationship(boundary, template, sourceID); rel != nil {
		relationships = append(relationships, *rel)
	}
	relationships = append(relationships, templateTargetRelationships(boundary, template, sourceID)...)
	if rel := templateLogGroupRelationship(boundary, template, sourceID); rel != nil {
		relationships = append(relationships, *rel)
	}
	if rel := templateS3Relationship(boundary, template, sourceID); rel != nil {
		relationships = append(relationships, *rel)
	}
	relationships = append(relationships, templateStopConditionRelationships(boundary, template, sourceID)...)
	return relationships
}

// templateIAMRoleRelationship records the IAM role an experiment template
// assumes. FIS reports a role ARN, matching how the IAM scanner publishes its
// role resource_id. It returns nil when no role ARN is reported or it is not an
// ARN.
func templateIAMRoleRelationship(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
) *awscloud.RelationshipObservation {
	roleARN := strings.TrimSpace(template.RoleARN)
	if roleARN == "" || !isARN(roleARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFISTemplateUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(template.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipFISTemplateUsesIAMRole + ":" + roleARN,
	}
}

// templateTargetRelationships records the explicit resource targets a template
// lists by ARN. Each ARN is typed to its resource family and keyed to the
// identity that family's scanner publishes: EC2 instances by the bare instance
// id, ECS clusters and RDS resources by ARN. Targets selected only by tag or
// filter (no explicit ARN) and ARNs from unmodeled families are skipped.
func templateTargetRelationships(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
) []awscloud.RelationshipObservation {
	seen := make(map[string]struct{})
	var relationships []awscloud.RelationshipObservation
	for _, target := range template.Targets {
		for _, resourceARN := range target.ResourceARNs {
			rel := targetResourceRelationship(boundary, template, sourceID, target, resourceARN)
			if rel == nil {
				continue
			}
			if _, exists := seen[rel.SourceRecordID]; exists {
				continue
			}
			seen[rel.SourceRecordID] = struct{}{}
			relationships = append(relationships, *rel)
		}
	}
	return relationships
}

// targetResourceRelationship types one explicit target ARN to its resource
// family and keys the edge to the published identity, or returns nil when the
// ARN belongs to a resource family this scanner does not model.
func targetResourceRelationship(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
	target Target,
	resourceARN string,
) *awscloud.RelationshipObservation {
	resourceARN = strings.TrimSpace(resourceARN)
	if !isARN(resourceARN) {
		return nil
	}
	attributes := targetAttributes(target)
	switch arnService(resourceARN) {
	case "ec2":
		instanceID := instanceIDFromARN(resourceARN)
		if instanceID == "" {
			return nil
		}
		// The EC2 instance node is keyed by the bare instance id, not an ARN, so
		// target_arn must stay empty (relguard rejects a bare join key alongside a
		// populated ARN). The full ARN is preserved as an edge attribute instead.
		ec2Attributes := withInstanceARN(attributes, resourceARN)
		return newTargetRelationship(
			boundary, template, sourceID,
			awscloud.RelationshipFISTemplateTargetsEC2Instance,
			instanceID, "", ec2InstanceTargetType, ec2Attributes,
		)
	case "ecs":
		if !strings.HasPrefix(arnResourceSegment(resourceARN), "cluster/") {
			return nil
		}
		return newTargetRelationship(
			boundary, template, sourceID,
			awscloud.RelationshipFISTemplateTargetsECSCluster,
			resourceARN, resourceARN, awscloud.ResourceTypeECSCluster, attributes,
		)
	case "rds":
		segment := arnResourceSegment(resourceARN)
		switch {
		case strings.HasPrefix(segment, "db:"):
			return newTargetRelationship(
				boundary, template, sourceID,
				awscloud.RelationshipFISTemplateTargetsRDSDBInstance,
				resourceARN, resourceARN, awscloud.ResourceTypeRDSDBInstance, attributes,
			)
		case strings.HasPrefix(segment, "cluster:"):
			return newTargetRelationship(
				boundary, template, sourceID,
				awscloud.RelationshipFISTemplateTargetsRDSDBCluster,
				resourceARN, resourceARN, awscloud.ResourceTypeRDSDBCluster, attributes,
			)
		default:
			return nil
		}
	default:
		return nil
	}
}

// newTargetRelationship constructs a template->target relationship with a
// stable, idempotent SourceRecordID keyed on the relationship type and target
// id so duplicate target ARNs do not create duplicate edges.
func newTargetRelationship(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID, relationshipType, targetID, targetARN, targetType string,
	attributes map[string]any,
) *awscloud.RelationshipObservation {
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(template.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + targetID,
	}
}

// targetAttributes records the non-secret FIS target selector context (the
// target key, FIS resource-type selector, and selection mode) as edge
// attributes, or nil when none are set. Filter values and resource-tag
// selectors are intentionally excluded.
func targetAttributes(target Target) map[string]any {
	attributes := map[string]any{}
	if key := strings.TrimSpace(target.Key); key != "" {
		attributes["target_key"] = key
	}
	if resourceType := strings.TrimSpace(target.ResourceType); resourceType != "" {
		attributes["fis_resource_type"] = resourceType
	}
	if mode := strings.TrimSpace(target.SelectionMode); mode != "" {
		attributes["selection_mode"] = mode
	}
	if len(attributes) == 0 {
		return nil
	}
	return attributes
}

// withInstanceARN returns a copy of base carrying the EC2 instance ARN under
// instance_arn so the full ARN is preserved on the edge even though the join key
// is the bare instance id. It never mutates base.
func withInstanceARN(base map[string]any, instanceARN string) map[string]any {
	instanceARN = strings.TrimSpace(instanceARN)
	out := make(map[string]any, len(base)+1)
	for key, value := range base {
		out[key] = value
	}
	if instanceARN != "" {
		out["instance_arn"] = instanceARN
	}
	return out
}

// templateLogGroupRelationship records the CloudWatch Logs log group experiment
// logs stream to. FIS reports a log group ARN, matching how the cloudwatchlogs
// scanner publishes its log group resource_id. It returns nil when no log group
// is configured. FIS log group ARNs carry a trailing :* wildcard, which is
// trimmed so the edge keys the bare log group ARN the cloudwatchlogs node
// publishes.
func templateLogGroupRelationship(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
) *awscloud.RelationshipObservation {
	logGroupARN := strings.TrimSuffix(strings.TrimSpace(template.LogGroupARN), ":*")
	if logGroupARN == "" || !isARN(logGroupARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFISTemplateLogsToCloudWatchLogGroup,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(template.ARN),
		TargetResourceID: logGroupARN,
		TargetARN:        logGroupARN,
		TargetType:       awscloud.ResourceTypeCloudWatchLogsLogGroup,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipFISTemplateLogsToCloudWatchLogGroup + ":" + logGroupARN,
	}
}

// templateS3Relationship records the S3 bucket experiment logs are written to.
// FIS reports a bucket name, so the scanner synthesizes the partition-aware
// bucket ARN to match the S3 scanner's published bucket resource_id. It returns
// nil when no S3 log destination is configured.
func templateS3Relationship(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
) *awscloud.RelationshipObservation {
	bucket := strings.TrimSpace(template.LogS3Bucket)
	if bucket == "" {
		return nil
	}
	bucketARN := arnForBucket(awscloud.PartitionForBoundary(boundary), bucket)
	if bucketARN == "" {
		return nil
	}
	attributes := map[string]any{"bucket": bucket}
	if prefix := strings.TrimSpace(template.LogS3Prefix); prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipFISTemplateLogsToS3,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(template.ARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipFISTemplateLogsToS3 + ":" + bucketARN,
	}
}

// templateStopConditionRelationships records the CloudWatch alarms an
// experiment template halts on. FIS reports an alarm ARN per cloudwatch-alarm
// stop condition, matching how the cloudwatch scanner publishes its alarm
// resource_id. Non-ARN and the implicit "none" stop condition are skipped.
func templateStopConditionRelationships(
	boundary awscloud.Boundary,
	template ExperimentTemplate,
	sourceID string,
) []awscloud.RelationshipObservation {
	seen := make(map[string]struct{})
	var relationships []awscloud.RelationshipObservation
	for _, alarmARN := range template.StopConditionAlarmARNs {
		alarmARN = strings.TrimSpace(alarmARN)
		if !isARN(alarmARN) {
			continue
		}
		recordID := sourceID + "->" + awscloud.RelationshipFISTemplateStopsOnCloudWatchAlarm + ":" + alarmARN
		if _, exists := seen[recordID]; exists {
			continue
		}
		seen[recordID] = struct{}{}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipFISTemplateStopsOnCloudWatchAlarm,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(template.ARN),
			TargetResourceID: alarmARN,
			TargetARN:        alarmARN,
			TargetType:       awscloud.ResourceTypeCloudWatchAlarm,
			SourceRecordID:   recordID,
		})
	}
	return relationships
}

// ec2InstanceTargetType is the target_type for the template-targets-EC2-instance
// edge. It mirrors the value other scanners use and the relguard
// KnownTargetTypeAllowlist forward-reference entry "aws_ec2_instance"; no EC2
// instance resource scanner exists yet, so the edge is a documented forward
// reference keyed by the bare instance id.
const ec2InstanceTargetType = "aws_ec2_instance"
