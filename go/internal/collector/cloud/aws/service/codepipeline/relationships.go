// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codepipeline

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
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
func pipelineRelationships(boundary aws.Boundary, pipeline Pipeline) []aws.RelationshipObservation {
	pipelineArnValue := firstNonEmpty(pipeline.ARN, pipelineARN(boundary, pipeline.Name))
	pipelineID := firstNonEmpty(pipelineArnValue, pipeline.Name)
	if pipelineID == "" {
		return nil
	}

	var observations []aws.RelationshipObservation

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
	boundary aws.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (aws.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(pipeline.RoleARN)
	if roleARN == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodePipelinePipelineUsesIAMRole,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   pipelineID + "#role#" + roleARN,
	}, true
}

func artifactBucketRelationship(
	boundary aws.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (aws.RelationshipObservation, bool) {
	bucket := strings.TrimSpace(pipeline.ArtifactStore.S3Bucket)
	if bucket == "" {
		return aws.RelationshipObservation{}, false
	}
	bucketARN := s3BucketARN(boundary, bucket)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodePipelinePipelineStoresArtifactsInS3Bucket,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		Attributes:       map[string]any{"bucket_name": bucket},
		SourceRecordID:   pipelineID + "#artifact-bucket#" + bucket,
	}, true
}

func artifactKeyRelationship(
	boundary aws.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) (aws.RelationshipObservation, bool) {
	keyID := strings.TrimSpace(pipeline.ArtifactStore.KMSKeyID)
	if keyID == "" {
		return aws.RelationshipObservation{}, false
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
	targetType := aws.ResourceTypeKMSKey
	if strings.Contains(keyID, ":alias/") {
		targetType = aws.ResourceTypeKMSAlias
	}
	targetARN := ""
	if strings.HasPrefix(keyID, "arn:") {
		targetARN = keyID
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodePipelinePipelineEncryptsArtifactsWithKMSKey,
		SourceResourceID: pipelineID,
		SourceARN:        pipelineArnValue,
		TargetResourceID: target,
		TargetARN:        targetARN,
		TargetType:       targetType,
		SourceRecordID:   pipelineID + "#artifact-key#" + target,
	}, true
}

func actionRelationships(
	boundary aws.Boundary,
	pipeline Pipeline,
	pipelineArnValue, pipelineID string,
) []aws.RelationshipObservation {
	var observations []aws.RelationshipObservation
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

			observations = append(observations, aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipCodePipelineStageContainsAction,
				SourceResourceID: pipelineID,
				SourceARN:        pipelineArnValue,
				TargetResourceID: actionID,
				TargetType:       aws.ResourceTypeCodePipelinePipeline,
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
	boundary aws.Boundary,
	action Action,
	pipelineArnValue, pipelineID, actionID string,
) (aws.RelationshipObservation, bool) {
	provider := strings.TrimSpace(action.SourceProvider)
	if provider == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodePipelineActionUsesSourceProvider,
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
	boundary aws.Boundary,
	action Action,
	pipelineArnValue, pipelineID, actionID string,
) (aws.RelationshipObservation, bool) {
	name := strings.TrimSpace(action.TargetResourceName)
	if name == "" {
		return aws.RelationshipObservation{}, false
	}
	switch strings.TrimSpace(action.TargetProvider) {
	case "CodeBuild":
		arn := codeBuildProjectARN(boundary, name)
		return targetRelationship(boundary, aws.RelationshipCodePipelineActionTargetsCodeBuildProject,
			aws.ResourceTypeCodeBuildProject, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "CodeDeploy":
		arn := codeDeployApplicationARN(boundary, name)
		return targetRelationship(boundary, aws.RelationshipCodePipelineActionTargetsCodeDeployApplication,
			aws.ResourceTypeCodeDeployApplication, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "Lambda":
		arn := lambdaFunctionARN(boundary, name)
		return targetRelationship(boundary, aws.RelationshipCodePipelineActionTargetsLambdaFunction,
			aws.ResourceTypeLambdaFunction, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, nil), true
	case "CloudFormation":
		// The CloudFormation scanner's stack node carries the stack name in its
		// correlation anchors. CodePipeline reports only the stack name (the
		// real stack id ARN has an account-generated UUID suffix this scanner
		// cannot know), so target the stack name to join the stack node by its
		// name anchor; leave target_arn empty rather than emit a wrong ARN.
		return targetRelationship(boundary, aws.RelationshipCodePipelineActionTargetsCloudFormationStack,
			aws.ResourceTypeCloudFormationStack, pipelineArnValue, pipelineID, actionID, name, "", action, nil), true
	case "ECS":
		cluster, service, ok := splitClusterService(name)
		if !ok {
			return aws.RelationshipObservation{}, false
		}
		arn := ecsServiceARN(boundary, cluster, service)
		attrs := map[string]any{"cluster_name": cluster, "service_name": service}
		return targetRelationship(boundary, aws.RelationshipCodePipelineActionTargetsECSService,
			aws.ResourceTypeECSService, pipelineArnValue, pipelineID, actionID, firstNonEmpty(arn, name), arn, action, attrs), true
	default:
		return aws.RelationshipObservation{}, false
	}
}

func targetRelationship(
	boundary aws.Boundary,
	relType, targetType, pipelineArnValue, pipelineID, actionID, targetID, targetARN string,
	action Action,
	extra map[string]any,
) aws.RelationshipObservation {
	attrs := map[string]any{"action_name": strings.TrimSpace(action.Name)}
	for key, value := range extra {
		attrs[key] = value
	}
	return aws.RelationshipObservation{
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
	boundary aws.Boundary,
	webhook Webhook,
) (aws.RelationshipObservation, bool) {
	pipelineName := strings.TrimSpace(webhook.TargetPipeline)
	webhookID := firstNonEmpty(strings.TrimSpace(webhook.ARN), strings.TrimSpace(webhook.Name))
	if pipelineName == "" || webhookID == "" {
		return aws.RelationshipObservation{}, false
	}
	pipelineArnValue := pipelineARN(boundary, pipelineName)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCodePipelineWebhookTriggersPipeline,
		SourceResourceID: webhookID,
		SourceARN:        strings.TrimSpace(webhook.ARN),
		TargetResourceID: firstNonEmpty(pipelineArnValue, pipelineName),
		TargetARN:        pipelineArnValue,
		TargetType:       aws.ResourceTypeCodePipelinePipeline,
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
func s3BucketARN(boundary aws.Boundary, bucket string) string {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:s3:::%s", aws.PartitionForBoundary(boundary), bucket)
}

func codeBuildProjectARN(boundary aws.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codebuild:%s:%s:project/%s",
		aws.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func codeDeployApplicationARN(boundary aws.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:application:%s",
		aws.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

func lambdaFunctionARN(boundary aws.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:lambda:%s:%s:function:%s",
		aws.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

// ecsServiceARN builds the Amazon ECS service ARN. The ECS scanner emits its
// service resource_id as this ARN, so the deploy-action edge targets the same
// ARN to join the ECS service node. CodePipeline reports the target as a
// cluster/service pair, never a bare service name.
func ecsServiceARN(boundary aws.Boundary, cluster, service string) string {
	cluster = strings.TrimSpace(cluster)
	service = strings.TrimSpace(service)
	if cluster == "" || service == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:ecs:%s:%s:service/%s/%s",
		aws.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, cluster, service)
}
