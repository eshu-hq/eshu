// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// instanceTargetRelationship records that an EC2 instance recommendation
// analyzes a specific instance. EC2 instance relationship targets are keyed by
// the bare instance id (i-...), not the ARN, so the scanner extracts the id from
// the analyzed instance ARN to join the instance identity other scanners
// publish. It returns nil when the source recommendation id or the bare instance
// id cannot be resolved, so the edge never dangles.
func instanceTargetRelationship(
	boundary awscloud.Boundary,
	rec InstanceRecommendation,
) *awscloud.RelationshipObservation {
	sourceID := instanceRecommendationID(rec)
	instanceID := instanceIDFromARN(rec.InstanceARN)
	if sourceID == "" || instanceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipComputeOptimizerRecommendationTargetsInstance,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(rec.InstanceARN),
		TargetResourceID: instanceID,
		TargetType:       "aws_ec2_instance",
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipComputeOptimizerRecommendationTargetsInstance + ":" + instanceID,
	}
}

// autoScalingGroupTargetRelationship records that an Auto Scaling group
// recommendation analyzes a specific group. The autoscaling scanner publishes
// its group resource_id as the group NAME (not the ARN), so the edge is keyed by
// name to join that node and leaves target_arn unset. The analyzed group ARN is
// carried as edge attribute evidence instead. It returns nil when the source
// recommendation id or the group name is missing.
func autoScalingGroupTargetRelationship(
	boundary awscloud.Boundary,
	rec AutoScalingGroupRecommendation,
) *awscloud.RelationshipObservation {
	sourceID := autoScalingGroupRecommendationID(rec)
	groupName := strings.TrimSpace(rec.AutoScalingGroupName)
	if sourceID == "" || groupName == "" {
		return nil
	}
	var attributes map[string]any
	if groupARN := strings.TrimSpace(rec.AutoScalingGroupARN); groupARN != "" {
		attributes = map[string]any{"auto_scaling_group_arn": groupARN}
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(rec.AutoScalingGroupARN),
		TargetResourceID: groupName,
		TargetType:       awscloud.ResourceTypeAutoScalingGroup,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup + ":" + groupName,
	}
}

// lambdaFunctionTargetRelationship records that a Lambda function recommendation
// analyzes a specific function. The lambda scanner publishes its function
// resource_id as the function ARN, so the edge is keyed by the analyzed function
// ARN to join that node. It returns nil when the source recommendation id or the
// function ARN is missing.
func lambdaFunctionTargetRelationship(
	boundary awscloud.Boundary,
	rec LambdaFunctionRecommendation,
) *awscloud.RelationshipObservation {
	sourceID := lambdaFunctionRecommendationID(rec)
	functionARN := strings.TrimSpace(rec.FunctionARN)
	if sourceID == "" || functionARN == "" {
		return nil
	}
	targetARN := ""
	if isARN(functionARN) {
		targetARN = functionARN
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipComputeOptimizerRecommendationTargetsFunction,
		SourceResourceID: sourceID,
		SourceARN:        functionARN,
		TargetResourceID: functionARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeLambdaFunction,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipComputeOptimizerRecommendationTargetsFunction + ":" + functionARN,
	}
}
