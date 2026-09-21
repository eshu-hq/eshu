// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// deploymentGroupRelationships derives the relationship observations CodeDeploy
// reports directly for one deployment group: parent application, service role,
// named compute targets, and SNS triggers. EC2 and on-premises tag filters are
// not relationships because a tag filter names no concrete resource; those
// summaries stay on the deployment-group resource attributes.
func deploymentGroupRelationships(
	boundary aws.Boundary,
	group DeploymentGroup,
) []aws.RelationshipObservation {
	groupARN := deploymentGroupARN(boundary, group.ApplicationName, group.Name)
	groupID := firstNonEmpty(groupARN, group.Name)
	if groupID == "" {
		return nil
	}

	var observations []aws.RelationshipObservation

	if rel, ok := applicationRelationship(boundary, group, groupARN, groupID); ok {
		observations = append(observations, rel)
	}
	if rel, ok := serviceRoleRelationship(boundary, group, groupARN, groupID); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, autoScalingGroupRelationships(boundary, group, groupARN, groupID)...)
	observations = append(observations, ecsServiceRelationships(boundary, group, groupARN, groupID)...)
	observations = append(observations, lambdaFunctionRelationships(boundary, group, groupARN, groupID)...)
	observations = append(observations, snsTriggerRelationships(boundary, group, groupARN, groupID)...)

	return observations
}

func applicationRelationship(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) (aws.RelationshipObservation, bool) {
	appName := strings.TrimSpace(group.ApplicationName)
	if appName == "" {
		return aws.RelationshipObservation{}, false
	}
	appARN := applicationARN(boundary, appName)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodeDeployDeploymentGroupBelongsToApplication,
		SourceResourceID: groupID,
		SourceARN:        groupARN,
		TargetResourceID: firstNonEmpty(appARN, appName),
		TargetARN:        appARN,
		TargetType:       aws.ResourceTypeCodeDeployApplication,
		SourceRecordID:   groupID + "#application#" + appName,
	}, true
}

func serviceRoleRelationship(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) (aws.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(group.ServiceRoleARN)
	if roleARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodeDeployDeploymentGroupUsesIAMRole,
		SourceResourceID: groupID,
		SourceARN:        groupARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   groupID + "#service-role#" + roleARN,
	}, true
}

func autoScalingGroupRelationships(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
	for _, asg := range group.AutoScalingGroups {
		name := strings.TrimSpace(asg)
		if name == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCodeDeployDeploymentGroupTargetsAutoScalingGroup,
			SourceResourceID: groupID,
			SourceARN:        groupARN,
			TargetResourceID: name,
			TargetType:       aws.ResourceTypeAutoScalingGroup,
			SourceRecordID:   groupID + "#asg#" + name,
		})
	}
	return observations
}

func ecsServiceRelationships(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
	for _, service := range group.ECSServices {
		cluster := strings.TrimSpace(service.ClusterName)
		name := strings.TrimSpace(service.ServiceName)
		if cluster == "" || name == "" {
			continue
		}
		serviceARN := ecsServiceARN(boundary, cluster, name)
		// Join against the ECS service node, whose resource_id is the service
		// ARN. Fall back to the cluster/service pair only when the ARN cannot
		// be built so the edge still carries a stable record id.
		target := firstNonEmpty(serviceARN, cluster+"/"+name)
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCodeDeployDeploymentGroupTargetsECSService,
			SourceResourceID: groupID,
			SourceARN:        groupARN,
			TargetResourceID: target,
			TargetARN:        serviceARN,
			TargetType:       aws.ResourceTypeECSService,
			Attributes: map[string]any{
				"cluster_name": cluster,
				"service_name": name,
			},
			SourceRecordID: groupID + "#ecs#" + target,
		})
	}
	return observations
}

func lambdaFunctionRelationships(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
	for _, function := range group.LambdaFunctions {
		name := strings.TrimSpace(function)
		if name == "" {
			continue
		}
		functionARN := lambdaFunctionARN(boundary, name)
		// Join against the Lambda function node, whose resource_id is the
		// function ARN. Fall back to the bare name only when the ARN cannot be
		// built.
		target := firstNonEmpty(functionARN, name)
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCodeDeployDeploymentGroupTargetsLambdaFunction,
			SourceResourceID: groupID,
			SourceARN:        groupARN,
			TargetResourceID: target,
			TargetARN:        functionARN,
			TargetType:       aws.ResourceTypeLambdaFunction,
			SourceRecordID:   groupID + "#lambda#" + name,
		})
	}
	return observations
}

func snsTriggerRelationships(
	boundary aws.Boundary,
	group DeploymentGroup,
	groupARN, groupID string,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
	for _, trigger := range group.SNSTriggers {
		topicARN := strings.TrimSpace(trigger.TopicARN)
		if topicARN == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCodeDeployDeploymentGroupNotifiesSNSTopic,
			SourceResourceID: groupID,
			SourceARN:        groupARN,
			TargetResourceID: topicARN,
			TargetARN:        topicARN,
			TargetType:       aws.ResourceTypeSNSTopic,
			Attributes: map[string]any{
				"trigger_name":   strings.TrimSpace(trigger.Name),
				"trigger_events": cloneStrings(trigger.Events),
			},
			SourceRecordID: groupID + "#sns#" + topicARN,
		})
	}
	return observations
}
