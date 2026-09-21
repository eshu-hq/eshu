// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodeDeploy identifies the regional AWS CodeDeploy metadata scan
	// slice.
	ServiceCodeDeploy = "codedeploy"
)

const (
	// ResourceTypeCodeDeployApplication identifies a CodeDeploy application.
	ResourceTypeCodeDeployApplication = "aws_codedeploy_application"
	// ResourceTypeCodeDeployDeploymentGroup identifies a CodeDeploy deployment
	// group.
	ResourceTypeCodeDeployDeploymentGroup = "aws_codedeploy_deployment_group"
	// ResourceTypeCodeDeployDeploymentConfig identifies a CodeDeploy deployment
	// configuration.
	ResourceTypeCodeDeployDeploymentConfig = "aws_codedeploy_deployment_config"
	// ResourceTypeCodeDeployDeployment identifies a single CodeDeploy
	// deployment observed from recent deployment metadata.
	ResourceTypeCodeDeployDeployment = "aws_codedeploy_deployment"
)

const (
	// ResourceTypeAutoScalingGroup identifies an EC2 Auto Scaling group used as
	// a relationship target. CodeDeploy is the first scanner to reference Auto
	// Scaling groups, so the shared target type lives here until another
	// service emits an Auto Scaling group resource of its own.
	ResourceTypeAutoScalingGroup = "aws_autoscaling_group"
)

const (
	// RelationshipCodeDeployDeploymentGroupBelongsToApplication records the
	// parent application a deployment group is defined under.
	RelationshipCodeDeployDeploymentGroupBelongsToApplication = "codedeploy_deployment_group_belongs_to_application"
	// RelationshipCodeDeployDeploymentGroupUsesIAMRole records the CodeDeploy
	// service role a deployment group assumes.
	RelationshipCodeDeployDeploymentGroupUsesIAMRole = "codedeploy_deployment_group_uses_iam_role"
	// RelationshipCodeDeployDeploymentGroupTargetsAutoScalingGroup records an
	// Auto Scaling group named as a deployment target.
	RelationshipCodeDeployDeploymentGroupTargetsAutoScalingGroup = "codedeploy_deployment_group_targets_auto_scaling_group"
	// RelationshipCodeDeployDeploymentGroupTargetsECSService records an Amazon
	// ECS cluster/service pair named as a deployment target.
	RelationshipCodeDeployDeploymentGroupTargetsECSService = "codedeploy_deployment_group_targets_ecs_service"
	// RelationshipCodeDeployDeploymentGroupTargetsLambdaFunction records a
	// Lambda function named as a deployment target.
	RelationshipCodeDeployDeploymentGroupTargetsLambdaFunction = "codedeploy_deployment_group_targets_lambda_function"
	// RelationshipCodeDeployDeploymentGroupNotifiesSNSTopic records an SNS
	// topic wired to a deployment-group trigger configuration.
	RelationshipCodeDeployDeploymentGroupNotifiesSNSTopic = "codedeploy_deployment_group_notifies_sns_topic"
)
