// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceImageBuilder identifies the regional EC2 Image Builder metadata-only
	// scan slice. The scanner reads image pipeline, image recipe, container
	// recipe, infrastructure configuration, and distribution configuration
	// control-plane metadata through the imagebuilder list/get APIs and never
	// reads or persists component build-document bodies, Dockerfile bodies, user
	// data, or any build artifact, and never mutates Image Builder state.
	ServiceImageBuilder = "imagebuilder"
)

const (
	// ResourceTypeImageBuilderImagePipeline identifies an EC2 Image Builder image
	// pipeline metadata resource. The scanner emits identity, status, platform,
	// the referenced recipe/config ARNs, and lifecycle timestamps only.
	ResourceTypeImageBuilderImagePipeline = "aws_imagebuilder_image_pipeline"
	// ResourceTypeImageBuilderImageRecipe identifies an EC2 Image Builder image
	// recipe metadata resource. The scanner emits identity, platform, version,
	// the parent image reference, and component ARN references only; component
	// build-document bodies and inline component data stay outside the contract.
	ResourceTypeImageBuilderImageRecipe = "aws_imagebuilder_image_recipe"
	// ResourceTypeImageBuilderContainerRecipe identifies an EC2 Image Builder
	// container recipe metadata resource. The scanner emits identity, container
	// type, platform, version, the parent image reference, the target ECR
	// repository name, and KMS key reference only; Dockerfile template bodies are
	// never read or persisted.
	ResourceTypeImageBuilderContainerRecipe = "aws_imagebuilder_container_recipe"
	// ResourceTypeImageBuilderInfrastructureConfiguration identifies an EC2 Image
	// Builder infrastructure configuration metadata resource. The scanner emits
	// identity, instance types, the instance profile name, networking references,
	// the SNS topic, and the S3 logging location only.
	ResourceTypeImageBuilderInfrastructureConfiguration = "aws_imagebuilder_infrastructure_configuration"
	// ResourceTypeImageBuilderDistributionConfiguration identifies an EC2 Image
	// Builder distribution configuration metadata resource. The scanner emits
	// identity, the distribution target regions, and lifecycle timestamps only.
	ResourceTypeImageBuilderDistributionConfiguration = "aws_imagebuilder_distribution_configuration"
)

const (
	// RelationshipImageBuilderPipelineUsesImageRecipe records that an image
	// pipeline builds from a given image recipe. The target is keyed by the image
	// recipe ARN the recipe node publishes.
	RelationshipImageBuilderPipelineUsesImageRecipe = "imagebuilder_pipeline_uses_image_recipe"
	// RelationshipImageBuilderPipelineUsesContainerRecipe records that an image
	// pipeline builds from a given container recipe. The target is keyed by the
	// container recipe ARN the recipe node publishes.
	RelationshipImageBuilderPipelineUsesContainerRecipe = "imagebuilder_pipeline_uses_container_recipe"
	// RelationshipImageBuilderPipelineUsesInfrastructureConfiguration records that
	// an image pipeline runs on a given infrastructure configuration, keyed by the
	// infrastructure configuration ARN that node publishes.
	RelationshipImageBuilderPipelineUsesInfrastructureConfiguration = "imagebuilder_pipeline_uses_infrastructure_configuration"
	// RelationshipImageBuilderPipelineUsesDistributionConfiguration records that
	// an image pipeline distributes through a given distribution configuration,
	// keyed by the distribution configuration ARN that node publishes.
	RelationshipImageBuilderPipelineUsesDistributionConfiguration = "imagebuilder_pipeline_uses_distribution_configuration"
	// RelationshipImageBuilderPipelineUsesExecutionRole records that an image
	// pipeline assumes a given IAM execution role to run its build workflows. The
	// target is keyed by the role ARN the IAM scanner publishes.
	RelationshipImageBuilderPipelineUsesExecutionRole = "imagebuilder_pipeline_uses_execution_role"
	// RelationshipImageBuilderInfraConfigUsesInstanceProfile records that an
	// infrastructure configuration launches build instances with a given IAM
	// instance profile. AWS reports the instance profile NAME, so the scanner
	// synthesizes the partition-aware instance-profile ARN the IAM scanner
	// publishes as the join key.
	RelationshipImageBuilderInfraConfigUsesInstanceProfile = "imagebuilder_infra_config_uses_instance_profile"
	// RelationshipImageBuilderInfraConfigUsesSubnet records an infrastructure
	// configuration's build-instance subnet. The target is keyed by the bare
	// subnet id (subnet-...) the VPC scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSubnet = "imagebuilder_infra_config_uses_subnet"
	// RelationshipImageBuilderInfraConfigUsesSecurityGroup records an
	// infrastructure configuration's build-instance security group. The target is
	// keyed by the bare security-group id (sg-...) the EC2 scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSecurityGroup = "imagebuilder_infra_config_uses_security_group"
	// RelationshipImageBuilderInfraConfigUsesSNSTopic records an infrastructure
	// configuration's build-status SNS topic. AWS reports the topic ARN, which is
	// the resource_id the SNS scanner publishes.
	RelationshipImageBuilderInfraConfigUsesSNSTopic = "imagebuilder_infra_config_uses_sns_topic"
	// RelationshipImageBuilderInfraConfigLogsToS3 records an infrastructure
	// configuration's build-log S3 bucket. AWS reports a bucket NAME, so the
	// scanner synthesizes the partition-aware bucket ARN (arn:<partition>:s3:::
	// <bucket>) the S3 scanner publishes as the join key.
	RelationshipImageBuilderInfraConfigLogsToS3 = "imagebuilder_infra_config_logs_to_s3"
	// RelationshipImageBuilderContainerRecipeUsesECRRepository records that a
	// container recipe pushes built images to a given ECR repository. AWS reports
	// a repository NAME, so the scanner synthesizes the partition-aware ECR
	// repository ARN the ECR scanner publishes as the join key.
	RelationshipImageBuilderContainerRecipeUsesECRRepository = "imagebuilder_container_recipe_uses_ecr_repository"
	// RelationshipImageBuilderContainerRecipeUsesKMSKey records a container
	// recipe's reported KMS encryption key dependency. The reported key id/ARN/
	// alias is the join key the KMS scanner publishes; target_arn is set only for
	// ARN-shaped identifiers.
	RelationshipImageBuilderContainerRecipeUsesKMSKey = "imagebuilder_container_recipe_uses_kms_key"
)
