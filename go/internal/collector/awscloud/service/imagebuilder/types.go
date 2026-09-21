// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only EC2 Image Builder observations for one AWS
// claim. Implementations read control-plane metadata through the imagebuilder
// list/get APIs and never read or persist component build-document bodies,
// Dockerfile bodies, user data, or any build artifact.
type Client interface {
	// Snapshot returns every Image Builder pipeline, recipe, container recipe,
	// infrastructure configuration, and distribution configuration visible to the
	// configured AWS credentials, plus non-fatal scan warnings.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures EC2 Image Builder control-plane metadata plus non-fatal
// scan warnings.
type Snapshot struct {
	// Pipelines is the metadata-only set of image pipelines.
	Pipelines []ImagePipeline
	// ImageRecipes is the metadata-only set of account-owned image recipes.
	ImageRecipes []ImageRecipe
	// ContainerRecipes is the metadata-only set of account-owned container
	// recipes.
	ContainerRecipes []ContainerRecipe
	// InfrastructureConfigurations is the metadata-only set of infrastructure
	// configurations.
	InfrastructureConfigurations []InfrastructureConfiguration
	// DistributionConfigurations is the metadata-only set of distribution
	// configurations.
	DistributionConfigurations []DistributionConfiguration
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// ImagePipeline is the scanner-owned Image Builder pipeline model. It carries
// control-plane metadata and the ARNs of the recipe and configuration resources
// it references, never any component build-document body or build artifact.
type ImagePipeline struct {
	// ARN is the Amazon Resource Name that uniquely identifies the pipeline.
	ARN string
	// Name is the pipeline name.
	Name string
	// Description is the optional pipeline description.
	Description string
	// Status is the pipeline status (ENABLED or DISABLED).
	Status string
	// Platform is the pipeline platform (Linux or Windows).
	Platform string
	// ImageRecipeARN is the image recipe the pipeline builds from, when set.
	ImageRecipeARN string
	// ContainerRecipeARN is the container recipe the pipeline builds from, when
	// set. A pipeline references either an image recipe or a container recipe.
	ContainerRecipeARN string
	// InfrastructureConfigurationARN is the infrastructure configuration the
	// pipeline runs on.
	InfrastructureConfigurationARN string
	// DistributionConfigurationARN is the distribution configuration the pipeline
	// distributes through, when set.
	DistributionConfigurationARN string
	// ExecutionRoleARN is the IAM role the pipeline assumes to run its build
	// workflows, when set.
	ExecutionRoleARN string
	// EnhancedImageMetadataEnabled reports whether enhanced image metadata
	// collection is enabled.
	EnhancedImageMetadataEnabled bool
	// ScheduleExpression is the pipeline schedule cron/rate expression, when set.
	ScheduleExpression string
	// DateCreated is the AWS-reported creation timestamp string.
	DateCreated string
	// DateUpdated is the AWS-reported last-update timestamp string.
	DateUpdated string
	// Tags carries the pipeline resource tags.
	Tags map[string]string
}

// ImageRecipe is the scanner-owned Image Builder image recipe model. It carries
// control-plane metadata and the parent image reference, never any component
// build-document body or inline component data.
type ImageRecipe struct {
	// ARN is the Amazon Resource Name that uniquely identifies the image recipe.
	ARN string
	// Name is the image recipe name.
	Name string
	// Description is the optional image recipe description.
	Description string
	// Platform is the image recipe platform (Linux or Windows).
	Platform string
	// Version is the semantic version of the image recipe.
	Version string
	// Owner is the AWS account or service that owns the recipe.
	Owner string
	// ParentImage is the parent image identifier (an AMI id, AMI ARN, or managed
	// image ARN) the recipe builds on top of. There is no EC2 AMI resource type,
	// so this is emitted as an attribute, not an edge.
	ParentImage string
	// WorkingDirectory is the build working directory, when set.
	WorkingDirectory string
	// ComponentARNs are the ARNs of the components the recipe references, in
	// order. Component build-document bodies are never read.
	ComponentARNs []string
	// DateCreated is the AWS-reported creation timestamp string.
	DateCreated string
	// Tags carries the image recipe resource tags.
	Tags map[string]string
}

// ContainerRecipe is the scanner-owned Image Builder container recipe model. It
// carries control-plane metadata, the parent image reference, the target ECR
// repository name, and the KMS key reference, never any Dockerfile template
// body or inline component data.
type ContainerRecipe struct {
	// ARN is the Amazon Resource Name that uniquely identifies the container
	// recipe.
	ARN string
	// Name is the container recipe name.
	Name string
	// Description is the optional container recipe description.
	Description string
	// Platform is the container recipe platform (Linux or Windows).
	Platform string
	// Version is the semantic version of the container recipe.
	Version string
	// Owner is the AWS account or service that owns the recipe.
	Owner string
	// ContainerType is the container type (for example DOCKER).
	ContainerType string
	// ParentImage is the parent image identifier the recipe builds on top of.
	ParentImage string
	// WorkingDirectory is the build working directory, when set.
	WorkingDirectory string
	// TargetRepositoryName is the name of the ECR repository built images are
	// pushed to, when the target repository service is ECR.
	TargetRepositoryName string
	// TargetRepositoryService is the target repository service (for example ECR).
	TargetRepositoryService string
	// KMSKeyID is the identifier of the KMS key used to encrypt the container
	// recipe, when set. AWS may report a key id, key ARN, or alias here.
	KMSKeyID string
	// Encrypted reports whether the container recipe is encrypted.
	Encrypted bool
	// ComponentARNs are the ARNs of the components the recipe references, in
	// order. Component build-document bodies are never read.
	ComponentARNs []string
	// DateCreated is the AWS-reported creation timestamp string.
	DateCreated string
	// Tags carries the container recipe resource tags.
	Tags map[string]string
}

// InfrastructureConfiguration is the scanner-owned Image Builder infrastructure
// configuration model. It carries control-plane metadata, the IAM instance
// profile name, networking references, the SNS topic, and the S3 logging
// location, never instance user data.
type InfrastructureConfiguration struct {
	// ARN is the Amazon Resource Name that uniquely identifies the infrastructure
	// configuration.
	ARN string
	// Name is the infrastructure configuration name.
	Name string
	// Description is the optional infrastructure configuration description.
	Description string
	// InstanceProfileName is the IAM instance profile name build instances launch
	// with. AWS reports a bare name, not an ARN.
	InstanceProfileName string
	// InstanceTypes are the EC2 instance types build instances may use.
	InstanceTypes []string
	// KeyPairConfigured reports whether an EC2 key pair is configured. The key
	// pair name itself is omitted to avoid persisting an instance-access selector.
	KeyPairConfigured bool
	// SubnetID is the build-instance subnet id (subnet-...), when set.
	SubnetID string
	// SecurityGroupIDs are the build-instance security group ids (sg-...).
	SecurityGroupIDs []string
	// SNSTopicARN is the build-status SNS topic ARN, when set.
	SNSTopicARN string
	// LoggingS3BucketName is the build-log S3 bucket name, when set. AWS reports a
	// bucket name, not an ARN.
	LoggingS3BucketName string
	// LoggingS3KeyPrefix is the build-log S3 key prefix, when set.
	LoggingS3KeyPrefix string
	// TerminateInstanceOnFailure reports whether build instances terminate on
	// failure.
	TerminateInstanceOnFailure bool
	// DateCreated is the AWS-reported creation timestamp string.
	DateCreated string
	// DateUpdated is the AWS-reported last-update timestamp string.
	DateUpdated string
	// Tags carries the infrastructure configuration resource tags.
	Tags map[string]string
}

// DistributionConfiguration is the scanner-owned Image Builder distribution
// configuration model. It carries control-plane metadata and the distribution
// target regions only.
type DistributionConfiguration struct {
	// ARN is the Amazon Resource Name that uniquely identifies the distribution
	// configuration.
	ARN string
	// Name is the distribution configuration name.
	Name string
	// Description is the optional distribution configuration description.
	Description string
	// Regions are the AWS regions images are distributed to.
	Regions []string
	// DateCreated is the AWS-reported creation timestamp string.
	DateCreated string
	// DateUpdated is the AWS-reported last-update timestamp string.
	DateUpdated string
	// Tags carries the distribution configuration resource tags.
	Tags map[string]string
}

// parsedTime parses an AWS-reported timestamp string into a UTC time, returning
// the zero time when value is blank or unparseable. AWS Image Builder reports
// timestamps as RFC3339-shaped strings, so timestamp attributes stay consistent
// with the time-typed attributes other scanners emit.
func parsedTime(value string) time.Time {
	value = trimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
