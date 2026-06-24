// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCodeBuild identifies the regional AWS CodeBuild metadata scan
	// slice.
	ServiceCodeBuild = "codebuild"
)

const (
	// ResourceTypeCodeBuildProject identifies a CodeBuild build project.
	ResourceTypeCodeBuildProject = "aws_codebuild_project"
	// ResourceTypeCodeBuildReportGroup identifies a CodeBuild report group.
	ResourceTypeCodeBuildReportGroup = "aws_codebuild_report_group"
	// ResourceTypeCodeBuildBuild identifies a single CodeBuild build observed
	// from recent build metadata. Build logs are never read or persisted.
	ResourceTypeCodeBuildBuild = "aws_codebuild_build"
)

const (
	// RelationshipCodeBuildProjectUsesIAMRole records the service role a build
	// project assumes. The target is the IAM role ARN.
	RelationshipCodeBuildProjectUsesIAMRole = "codebuild_project_uses_iam_role"
	// RelationshipCodeBuildProjectUsesVPC records the VPC a build project runs
	// inside. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipCodeBuildProjectUsesVPC = "codebuild_project_uses_vpc"
	// RelationshipCodeBuildProjectUsesSubnet records a VPC subnet a build
	// project runs inside. The target is the EC2-owned aws_ec2_subnet identity.
	RelationshipCodeBuildProjectUsesSubnet = "codebuild_project_uses_subnet"
	// RelationshipCodeBuildProjectUsesSecurityGroup records a security group a
	// build project attaches. The target is the EC2-owned aws_ec2_security_group
	// identity.
	RelationshipCodeBuildProjectUsesSecurityGroup = "codebuild_project_uses_security_group"
	// RelationshipCodeBuildProjectUsesKMSKey records the KMS key a build project
	// uses to encrypt build output artifacts.
	RelationshipCodeBuildProjectUsesKMSKey = "codebuild_project_uses_kms_key"
	// RelationshipCodeBuildProjectSourcedFromS3 records an Amazon S3 bucket that
	// holds a build project's input source. The target is the S3 bucket ARN.
	RelationshipCodeBuildProjectSourcedFromS3 = "codebuild_project_sourced_from_s3"
	// RelationshipCodeBuildProjectSourcedFromRepository records a Git source
	// provider (GitHub, GitHub Enterprise, CodeCommit, Bitbucket, GitLab) a build
	// project pulls source from. The target is the reported repository location.
	RelationshipCodeBuildProjectSourcedFromRepository = "codebuild_project_sourced_from_repository"
	// RelationshipCodeBuildProjectArtifactsToS3 records an Amazon S3 bucket a
	// build project writes output artifacts to. The target is the S3 bucket ARN.
	RelationshipCodeBuildProjectArtifactsToS3 = "codebuild_project_artifacts_to_s3"
	// RelationshipCodeBuildProjectReferencesSecret records a Secrets Manager
	// secret referenced by a SECRETS_MANAGER environment variable. The target is
	// the Secrets Manager secret ARN or name.
	RelationshipCodeBuildProjectReferencesSecret = "codebuild_project_references_secret"
	// RelationshipCodeBuildProjectReferencesSSMParameter records an SSM Parameter
	// Store parameter referenced by a PARAMETER_STORE environment variable. The
	// target is the SSM parameter ARN or name.
	RelationshipCodeBuildProjectReferencesSSMParameter = "codebuild_project_references_ssm_parameter"
)
