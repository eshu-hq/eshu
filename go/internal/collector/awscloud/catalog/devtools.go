// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceCodeArtifact identifies the regional AWS CodeArtifact
	// metadata-only scan slice covering package-registry domains and
	// repositories. The scanner never reads, downloads, publishes, copies, or
	// deletes package versions or package assets; it observes only domain and
	// repository metadata plus the external-connection and upstream-repository
	// identifiers that AWS reports.
	ServiceCodeArtifact = "codeartifact"
)

const (
	// ResourceTypeCodeArtifactDomain identifies an AWS CodeArtifact domain
	// metadata resource. A domain is the container that owns repositories and
	// the KMS key that encrypts package assets. The scanner emits the domain
	// name, ARN, owner account, encryption-key ARN, repository count, and asset
	// size; it never reads package contents stored under the domain.
	ResourceTypeCodeArtifactDomain = "aws_codeartifact_domain"
	// ResourceTypeCodeArtifactRepository identifies an AWS CodeArtifact
	// repository metadata resource. The scanner emits the repository name, ARN,
	// owning domain, external-connection identifiers, and upstream-repository
	// identifiers; it never reads package versions or assets in the repository.
	ResourceTypeCodeArtifactRepository = "aws_codeartifact_repository"
)

const (
	// RelationshipCodeArtifactRepositoryInDomain records a CodeArtifact
	// repository's membership in the domain that contains it. The target is the
	// domain identified by its name, matching the domain scanner resource_id.
	RelationshipCodeArtifactRepositoryInDomain = "codeartifact_repository_in_domain"
	// RelationshipCodeArtifactDomainUsesKMSKey records the KMS key a
	// CodeArtifact domain uses to encrypt package assets. The target is the KMS
	// key identified by the encryption-key ARN AWS reports for the domain.
	RelationshipCodeArtifactDomainUsesKMSKey = "codeartifact_domain_uses_kms_key"
	// RelationshipCodeArtifactRepositoryUpstreamRepository records a
	// CodeArtifact repository's reported upstream repository. CodeArtifact
	// consults upstream repositories in priority order when resolving a
	// requested package version. The target is the upstream repository
	// identified within the same domain.
	RelationshipCodeArtifactRepositoryUpstreamRepository = "codeartifact_repository_upstream_repository"
	// RelationshipCodeArtifactRepositoryExternalConnection records a
	// CodeArtifact repository's external connection to a public package
	// registry (for example public:npmjs or public:pypi). The target is a
	// labeled non-AWS public-registry identity, not a scanned AWS resource.
	RelationshipCodeArtifactRepositoryExternalConnection = "codeartifact_repository_external_connection"
)

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
	RelationshipCodeBuildProjectReferencesSecret = "codebuild_project_references_secret" // #nosec G101 -- relationship-type identifier recording a Secrets Manager reference, not a credential value
	// RelationshipCodeBuildProjectReferencesSSMParameter records an SSM Parameter
	// Store parameter referenced by a PARAMETER_STORE environment variable. The
	// target is the SSM parameter ARN or name.
	RelationshipCodeBuildProjectReferencesSSMParameter = "codebuild_project_references_ssm_parameter"
)

const (
	// ServiceCodeCommit identifies the regional AWS CodeCommit metadata scan
	// slice. The scanner is metadata-only: it never reads commits, refs,
	// blobs, file contents, pull-request bodies, or comment text, and never
	// mutates any CodeCommit resource.
	ServiceCodeCommit = "codecommit"
)

const (
	// ResourceTypeCodeCommitRepository identifies an AWS CodeCommit repository
	// metadata resource. The scanner emits repository identity (name, ARN,
	// repository id), the default branch name, clone-URL host evidence, the
	// encryption KMS key id, and creation/last-modified timestamps. It never
	// emits commit, ref, blob, or file-content evidence. The repository
	// publishes correlation anchors (repository name and clone URLs) so a
	// CodeBuild project, CodePipeline source action, or Amplify app whose Git
	// source points at the repository joins this resource as the code-to-cloud
	// anchor.
	ResourceTypeCodeCommitRepository = "aws_codecommit_repository"
)

const (
	// RelationshipCodeCommitRepositoryEncryptedWithKMSKey records the KMS key a
	// CodeCommit repository is encrypted with. The target is the KMS key the
	// repository reports (a bare key id or a key ARN), typed aws_kms_key.
	RelationshipCodeCommitRepositoryEncryptedWithKMSKey = "codecommit_repository_encrypted_with_kms_key"
	// RelationshipCodeCommitRepositoryTriggersSNSTopic records an SNS topic wired
	// to a CodeCommit repository trigger. The target is the trigger destination
	// SNS topic ARN, typed aws_sns_topic. Only SNS-topic destinations produce an
	// edge; non-SNS trigger destinations (for example Lambda) are recorded as
	// resource attributes, not promoted to this relationship.
	RelationshipCodeCommitRepositoryTriggersSNSTopic = "codecommit_repository_triggers_sns_topic"
)

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

const (
	// ServiceCodeGuru identifies the regional Amazon CodeGuru metadata-only scan
	// slice that covers both CodeGuru Reviewer and CodeGuru Profiler. The scanner
	// reads control-plane metadata through ListRepositoryAssociations (Reviewer)
	// and ListProfilingGroups (Profiler) and never reads recommendation content,
	// code-review findings, profiling sample data, flame graphs, or agent
	// telemetry payloads, and never mutates any CodeGuru resource.
	ServiceCodeGuru = "codeguru"
)

const (
	// ResourceTypeCodeGuruRepositoryAssociation identifies a CodeGuru Reviewer
	// repository association metadata resource. The scanner emits association
	// identity (ARN, association id, name, owner, provider type, state) and the
	// configured-backing references (CodeStar connection ARN, S3 bucket name,
	// customer-managed KMS key id) only. It never emits code-review findings,
	// recommendation text, or any analyzed source content.
	ResourceTypeCodeGuruRepositoryAssociation = "aws_codeguru_repository_association"
	// ResourceTypeCodeGuruProfilingGroup identifies a CodeGuru Profiler profiling
	// group metadata resource. The scanner emits group identity (ARN, name,
	// compute platform, profiling-enabled posture, lifecycle timestamps) only. It
	// never emits profiling samples, aggregated profiles, flame graphs,
	// recommendation reports, or agent telemetry.
	ResourceTypeCodeGuruProfilingGroup = "aws_codeguru_profiling_group"
)

const (
	// RelationshipCodeGuruAssociationReviewsCodeCommitRepository records a
	// CodeGuru Reviewer repository association whose provider is AWS CodeCommit.
	// The target is the partition-aware CodeCommit repository ARN
	// (arn:<partition>:codecommit:<region>:<owner-account>:<name>), which matches
	// how the CodeCommit scanner publishes its repository resource_id, so the edge
	// joins the existing repository node. Associations for non-CodeCommit
	// providers (GitHub, Bitbucket, S3, GitHub Enterprise Server) record their
	// backing reference as a resource attribute and emit no edge, never a
	// dangling one.
	RelationshipCodeGuruAssociationReviewsCodeCommitRepository = "codeguru_association_reviews_codecommit_repository"
)

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
