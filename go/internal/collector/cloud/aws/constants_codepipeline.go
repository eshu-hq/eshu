// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodePipeline identifies the regional AWS CodePipeline metadata scan
	// slice.
	ServiceCodePipeline = "codepipeline"
)

const (
	// ResourceTypeCodePipelinePipeline identifies a CodePipeline pipeline.
	ResourceTypeCodePipelinePipeline = "aws_codepipeline_pipeline"
	// ResourceTypeCodePipelineExecution identifies one recent CodePipeline
	// pipeline execution observed from execution-summary metadata.
	ResourceTypeCodePipelineExecution = "aws_codepipeline_execution"
	// ResourceTypeCodePipelineWebhook identifies a CodePipeline webhook.
	ResourceTypeCodePipelineWebhook = "aws_codepipeline_webhook"
	// ResourceTypeCodePipelineActionType identifies a CodePipeline custom action
	// type.
	ResourceTypeCodePipelineActionType = "aws_codepipeline_action_type"
)

const (
	// RelationshipCodePipelinePipelineUsesIAMRole records the IAM service role a
	// pipeline assumes for its actions.
	RelationshipCodePipelinePipelineUsesIAMRole = "codepipeline_pipeline_uses_iam_role"
	// RelationshipCodePipelinePipelineStoresArtifactsInS3Bucket records an S3
	// artifact store bucket a pipeline writes artifacts to.
	RelationshipCodePipelinePipelineStoresArtifactsInS3Bucket = "codepipeline_pipeline_stores_artifacts_in_s3_bucket"
	// RelationshipCodePipelinePipelineEncryptsArtifactsWithKMSKey records a KMS
	// key a pipeline uses to encrypt its artifact store.
	RelationshipCodePipelinePipelineEncryptsArtifactsWithKMSKey = "codepipeline_pipeline_encrypts_artifacts_with_kms_key"
	// RelationshipCodePipelineStageContainsAction records a pipeline stage that
	// contains one declared action.
	RelationshipCodePipelineStageContainsAction = "codepipeline_stage_contains_action"
	// RelationshipCodePipelineActionUsesSourceProvider records a source action
	// and the source provider (S3, CodeCommit, GitHub via App, Bitbucket via
	// App) it reads from. It names the provider only; the provider's concrete
	// repository or bucket lives in action configuration values the scanner
	// never persists.
	RelationshipCodePipelineActionUsesSourceProvider = "codepipeline_action_uses_source_provider"
	// RelationshipCodePipelineActionTargetsCodeBuildProject records a build or
	// test action that runs an AWS CodeBuild project.
	RelationshipCodePipelineActionTargetsCodeBuildProject = "codepipeline_action_targets_codebuild_project"
	// RelationshipCodePipelineActionTargetsCodeDeployApplication records a deploy
	// action that targets an AWS CodeDeploy application.
	RelationshipCodePipelineActionTargetsCodeDeployApplication = "codepipeline_action_targets_codedeploy_application"
	// RelationshipCodePipelineActionTargetsLambdaFunction records an invoke
	// action that runs an AWS Lambda function.
	RelationshipCodePipelineActionTargetsLambdaFunction = "codepipeline_action_targets_lambda_function"
	// RelationshipCodePipelineActionTargetsCloudFormationStack records a deploy
	// action that targets an AWS CloudFormation stack.
	RelationshipCodePipelineActionTargetsCloudFormationStack = "codepipeline_action_targets_cloudformation_stack"
	// RelationshipCodePipelineActionTargetsECSService records a deploy action
	// that targets an Amazon ECS service.
	RelationshipCodePipelineActionTargetsECSService = "codepipeline_action_targets_ecs_service"
	// RelationshipCodePipelineWebhookTriggersPipeline records a webhook wired to
	// a pipeline through a named source (target) action.
	RelationshipCodePipelineWebhookTriggersPipeline = "codepipeline_webhook_triggers_pipeline"
)
