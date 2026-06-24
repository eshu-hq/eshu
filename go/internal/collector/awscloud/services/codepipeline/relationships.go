// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// ResourceTypeCodePipelineSourceProvider names the synthetic provider category
// a source-action edge points at. A source action's concrete repository or
// bucket lives in action configuration values the scanner never persists, so
// the edge documents the provider class (S3, CodeCommit, GitHub,
// CodeStarSourceConnection, Bitbucket) only and never carries an empty
// target_type.
const ResourceTypeCodePipelineSourceProvider = "aws_codepipeline_source_provider"

// pipelineRelationships derives every relationship a pipeline reports: the
// service role, the S3 artifact store, the KMS encryption key, stage->action
// containment, action->source-provider edges, and action->target edges for the
// build/deploy/invoke targets resolved from allowlisted non-secret
// configuration keys.
func pipelineRelationships(boundary awscloud.Boundary, pipeline Pipeline) []awscloud.RelationshipObservation {
	pipelineArnValue := firstNonEmpty(pipeline.ARN, pipelineARN(boundary, pipeline.Name))
	pipelineID := firstNonEmpty(pipelineArnValue, pipeline.Name)
	if pipelineID == "" {
		return nil
	}

	var observations []awscloud.RelationshipObservation

	if rel, ok := pipelineRoleRelationship(boundary, pipeline, pipelineArnValue, pipelineID); ok {
		observations = append(observations, rel)
	}
	if rel, ok := artifactBucketRelationship(boundary, pipeline, pipelineArnValue, pipelineID); ok {
		observations = append(observations, rel)
	}
	if rel, ok := artifactKeyRelationship(boundary, pipeline, pipelineArnValue, pipelineID); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, actionRelationships(boundary, pipeline, pipelineArnValue, pipelineID)...)

	return observations
}

func pipelineRoleRelationship(
	boundary awscloud.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(pipeline.RoleARN)
	if roleARN == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodePipelinePipelineUsesIAMRole,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   pipelineID + "#role#" + roleARN,
	}, true
}

func artifactBucketRelationship(
	boundary awscloud.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (awscloud.RelationshipObservation, bool) {
	bucket := strings.TrimSpace(pipeline.ArtifactStore.S3Bucket)
	if bucket == "" {
		return awscloud.RelationshipObservation{}, false
	}
	bucketARN := s3BucketARN(boundary, bucket)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodePipelinePipelineStoresArtifactsInS3Bucket,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       map[string]any{"bucket_name": bucket},
		SourceRecordID:   pipelineID + "#artifact-bucket#" + bucket,
	}, true
}

func artifactKeyRelationship(
	boundary awscloud.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (awscloud.RelationshipObservation, bool) {
	keyID := strings.TrimSpace(pipeline.ArtifactStore.KMSKeyID)
	if keyID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	// CodePipeline reports the encryption key as a bare key id, a key ARN, or an
	// alias ARN. The KMS scanner emits a key node (resource_id = bare key id or
	// key ARN, anchors [keyARN, keyID]) and a separate alias node (resource_id =
	// alias ARN or alias name, anchors [aliasARN, aliasName]). A KMS key never
	// carries an alias ARN in its anchors, so an alias-ARN reference must target
	// the alias node, not the key node, or the edge dangles. Detect the alias-ARN
	// shape (:alias/) and target aws_kms_alias for those; keep aws_kms_key for
	// key ARNs and bare key ids. Set target_arn only when the value is an ARN.
	target := keyID
	targetType := awscloud.ResourceTypeKMSKey
	if strings.Contains(keyID, ":alias/") {
		targetType = awscloud.ResourceTypeKMSAlias
	}
	targetARN := ""
	if strings.HasPrefix(keyID, "arn:") {
		targetARN = keyID
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodePipelinePipelineEncryptsArtifactsWithKMSKey,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: target,
		TargetARN:        targetARN,
		TargetType:       targetType,
		SourceRecordID:   pipelineID + "#artifact-key#" + target,
	}, true
}

func actionRelationships(
	boundary awscloud.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, stage := range pipeline.Stages {
		stageName := strings.TrimSpace(stage.Name)
		if stageName == "" {
			continue
		}
		stageID := pipelineID + "#stage#" + stageName
		for _, action := range stage.Actions {
			actionName := strings.TrimSpace(action.Name)
			if actionName == "" {
				continue
			}
			actionID := stageID + "#action#" + actionName

			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipCodePipelineStageContainsAction,
				SourceResourceID: pipelineID,
				SourceARN:        pipelineArnValue,
				TargetResourceID: actionID,
				TargetType:       awscloud.ResourceTypeCodePipelinePipeline,
				Attributes: map[string]any{
					"stage_name":  stageName,
					"action_name": actionName,
					"category":    strings.TrimSpace(action.Category),
					"provider":    strings.TrimSpace(action.Provider),
				},
				SourceRecordID: actionID,
			})

			if rel, ok := sourceProviderRelationship(boundary, action, pipelineArnValue, pipelineID, actionID); ok {
				observations = append(observations, rel)
			}
			if rel, ok := actionTargetRelationship(boundary, action, pipelineArnValue, pipelineID, actionID); ok {
				observations = append(observations, rel)
			}
		}
	}
	return observations
}

func sourceProviderRelationship(
	boundary awscloud.Boundary,
	action Action,
	pipelineArnValue, pipelineID, actionID string,
) (awscloud.RelationshipObservation, bool) {
	provider := strings.TrimSpace(action.SourceProvider)
	if provider == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodePipelineActionUsesSourceProvider,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: provider,
		TargetType:       ResourceTypeCodePipelineSourceProvider,
		Attributes: map[string]any{
			"action_name": strings.TrimSpace(action.Name),
			"provider":    provider,
		},
		SourceRecordID: actionID + "#source#" + provider,
	}, true
}

// actionTargetRelationship resolves the build/deploy/invoke target edge from
// the allowlisted non-secret target identifier the adapter read. It joins the
// target scanner's node by matching that scanner's resource_id, never an empty
// target_type. The target name came from a known identifier configuration key
// (ProjectName, ApplicationName, FunctionName, StackName, ClusterName +
// ServiceName), never from a secret configuration value.
func actionTargetRelationship(
	boundary awscloud.Boundary,
	action Action,
	pipelineArnValue, pipelineID, actionID string,
) (awscloud.RelationshipObservation, bool) {
	name := strings.TrimSpace(action.TargetResourceName)
	if name == "" {
		return awscloud.RelationshipObservation{}, false
	}
	switch strings.TrimSpace(action.TargetProvider) {
	case "CodeBuild":
		arn := codeBuildProjectARN(boundary, name)
		return targetRelationship(boundary, awscloud.RelationshipCodePipelineActionTargetsCodeBuildProject,
			awscloud.ResourceTypeCodeBuildProject, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "CodeDeploy":
		arn := codeDeployApplicationARN(boundary, name)
		return targetRelationship(boundary, awscloud.RelationshipCodePipelineActionTargetsCodeDeployApplication,
			awscloud.ResourceTypeCodeDeployApplication, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "Lambda":
		arn := lambdaFunctionARN(boundary, name)
		return targetRelationship(boundary, awscloud.RelationshipCodePipelineActionTargetsLambdaFunction,
			awscloud.ResourceTypeLambdaFunction, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "CloudFormation":
		// The CloudFormation scanner's stack node carries the stack name in its
		// correlation anchors. CodePipeline reports only the stack name (the
		// real stack id ARN has an account-generated UUID suffix this scanner
		// cannot know), so target the stack name to join the stack node by its
		// name anchor; leave target_arn empty rather than emit a wrong ARN.
		return targetRelationship(boundary, awscloud.RelationshipCodePipelineActionTargetsCloudFormationStack,
			awscloud.ResourceTypeCloudFormationStack, pipelineArnValue, pipelineID, actionID, name, "", action, nil), true
	case "ECS":
		cluster, service, ok := splitClusterService(name)
		if !ok {
			return awscloud.RelationshipObservation{}, false
		}
		arn := ecsServiceARN(boundary, cluster, service)
		attrs := map[string]any{"cluster_name": cluster, "service_name": service}
		return targetRelationship(boundary, awscloud.RelationshipCodePipelineActionTargetsECSService,
			awscloud.ResourceTypeECSService, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, attrs), true
	default:
		return awscloud.RelationshipObservation{}, false
	}
}

func targetRelationship(
	boundary awscloud.Boundary,
	relType, targetType, pipelineArnValue, pipelineID, actionID, targetID, targetARN string,
	action Action,
	extra map[string]any,
) awscloud.RelationshipObservation {
	attrs := map[string]any{"action_name": strings.TrimSpace(action.Name)}
	for key, value := range extra {
		attrs[key] = value
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relType,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attrs,
		SourceRecordID:   actionID + "#target#" + targetID,
	}
}

func webhookRelationship(
	boundary awscloud.Boundary,
	webhook Webhook,
) (awscloud.RelationshipObservation, bool) {
	pipelineName := strings.TrimSpace(webhook.TargetPipeline)
	webhookID := firstNonEmpty(strings.TrimSpace(webhook.ARN), strings.TrimSpace(webhook.Name))
	if pipelineName == "" || webhookID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	pipelineArnValue := pipelineARN(boundary, pipelineName)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCodePipelineWebhookTriggersPipeline,
		SourceResourceID: webhookID,
		SourceARN:        strings.TrimSpace(webhook.ARN),
		TargetResourceID: firstNonEmpty(pipelineArnValue, pipelineName),
		TargetARN:        pipelineArnValue,
		TargetType:       awscloud.ResourceTypeCodePipelinePipeline,
		Attributes: map[string]any{
			"target_action": strings.TrimSpace(webhook.TargetAction),
		},
		SourceRecordID: webhookID + "#triggers#" + pipelineName,
	}, true
}

func splitClusterService(value string) (cluster, service string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	cluster = strings.TrimSpace(parts[0])
	service = strings.TrimSpace(parts[1])
	if cluster == "" || service == "" {
		return "", "", false
	}
	return cluster, service, true
}

// s3BucketARN builds the S3 bucket ARN. The S3 scanner emits its bucket
// resource_id as the bucket ARN, so the artifact-store edge targets that ARN.
func s3BucketARN(boundary awscloud.Boundary, bucket string) string {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:s3:::%s", awscloud.PartitionForBoundary(boundary), bucket)
}

func codeBuildProjectARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codebuild:%s:%s:project/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func codeDeployApplicationARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:application:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func lambdaFunctionARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:lambda:%s:%s:function:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

// ecsServiceARN builds the Amazon ECS service ARN. The ECS scanner emits its
// service resource_id as this ARN, so the deploy-action edge targets the same
// ARN to join the ECS service node. CodePipeline reports the target as a
// cluster/service pair, never a bare service name.
func ecsServiceARN(boundary awscloud.Boundary, cluster, service string) string {
	cluster = strings.TrimSpace(cluster)
	service = strings.TrimSpace(service)
	if cluster == "" || service == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:ecs:%s:%s:service/%s/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, cluster, service)
}
