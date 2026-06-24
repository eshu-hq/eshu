// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsimagebuildertypes "github.com/aws/aws-sdk-go-v2/service/imagebuilder/types"

	imagebuilderservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/imagebuilder"
)

// mapPipeline maps an SDK image pipeline summary into the scanner-owned model.
func mapPipeline(pipeline awsimagebuildertypes.ImagePipeline) imagebuilderservice.ImagePipeline {
	return imagebuilderservice.ImagePipeline{
		ARN:                            strings.TrimSpace(aws.ToString(pipeline.Arn)),
		Name:                           strings.TrimSpace(aws.ToString(pipeline.Name)),
		Description:                    strings.TrimSpace(aws.ToString(pipeline.Description)),
		Status:                         strings.TrimSpace(string(pipeline.Status)),
		Platform:                       strings.TrimSpace(string(pipeline.Platform)),
		ImageRecipeARN:                 strings.TrimSpace(aws.ToString(pipeline.ImageRecipeArn)),
		ContainerRecipeARN:             strings.TrimSpace(aws.ToString(pipeline.ContainerRecipeArn)),
		InfrastructureConfigurationARN: strings.TrimSpace(aws.ToString(pipeline.InfrastructureConfigurationArn)),
		DistributionConfigurationARN:   strings.TrimSpace(aws.ToString(pipeline.DistributionConfigurationArn)),
		ExecutionRoleARN:               strings.TrimSpace(aws.ToString(pipeline.ExecutionRole)),
		EnhancedImageMetadataEnabled:   aws.ToBool(pipeline.EnhancedImageMetadataEnabled),
		ScheduleExpression:             scheduleExpression(pipeline.Schedule),
		DateCreated:                    strings.TrimSpace(aws.ToString(pipeline.DateCreated)),
		DateUpdated:                    strings.TrimSpace(aws.ToString(pipeline.DateUpdated)),
		Tags:                           pipeline.Tags,
	}
}

// scheduleExpression extracts the pipeline schedule cron/rate expression, never
// any other schedule field.
func scheduleExpression(schedule *awsimagebuildertypes.Schedule) string {
	if schedule == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(schedule.ScheduleExpression))
}

// mapImageRecipe maps an SDK image recipe into the scanner-owned model. The
// component ARNs are referenced; component build-document bodies are not read.
func mapImageRecipe(recipe awsimagebuildertypes.ImageRecipe) imagebuilderservice.ImageRecipe {
	return imagebuilderservice.ImageRecipe{
		ARN:              strings.TrimSpace(aws.ToString(recipe.Arn)),
		Name:             strings.TrimSpace(aws.ToString(recipe.Name)),
		Description:      strings.TrimSpace(aws.ToString(recipe.Description)),
		Platform:         strings.TrimSpace(string(recipe.Platform)),
		Version:          strings.TrimSpace(aws.ToString(recipe.Version)),
		Owner:            strings.TrimSpace(aws.ToString(recipe.Owner)),
		ParentImage:      strings.TrimSpace(aws.ToString(recipe.ParentImage)),
		WorkingDirectory: strings.TrimSpace(aws.ToString(recipe.WorkingDirectory)),
		ComponentARNs:    componentARNs(recipe.Components),
		DateCreated:      strings.TrimSpace(aws.ToString(recipe.DateCreated)),
		Tags:             recipe.Tags,
	}
}

// mapContainerRecipe maps an SDK container recipe into the scanner-owned model.
// The Dockerfile template body (DockerfileTemplateData) is never copied; only
// the target ECR repository name, KMS key reference, and component ARNs are kept.
func mapContainerRecipe(recipe awsimagebuildertypes.ContainerRecipe) imagebuilderservice.ContainerRecipe {
	mapped := imagebuilderservice.ContainerRecipe{
		ARN:              strings.TrimSpace(aws.ToString(recipe.Arn)),
		Name:             strings.TrimSpace(aws.ToString(recipe.Name)),
		Description:      strings.TrimSpace(aws.ToString(recipe.Description)),
		Platform:         strings.TrimSpace(string(recipe.Platform)),
		Version:          strings.TrimSpace(aws.ToString(recipe.Version)),
		Owner:            strings.TrimSpace(aws.ToString(recipe.Owner)),
		ContainerType:    strings.TrimSpace(string(recipe.ContainerType)),
		ParentImage:      strings.TrimSpace(aws.ToString(recipe.ParentImage)),
		WorkingDirectory: strings.TrimSpace(aws.ToString(recipe.WorkingDirectory)),
		KMSKeyID:         strings.TrimSpace(aws.ToString(recipe.KmsKeyId)),
		Encrypted:        aws.ToBool(recipe.Encrypted),
		ComponentARNs:    componentARNs(recipe.Components),
		DateCreated:      strings.TrimSpace(aws.ToString(recipe.DateCreated)),
		Tags:             recipe.Tags,
	}
	if repo := recipe.TargetRepository; repo != nil {
		mapped.TargetRepositoryName = strings.TrimSpace(aws.ToString(repo.RepositoryName))
		mapped.TargetRepositoryService = strings.TrimSpace(string(repo.Service))
	}
	return mapped
}

// mapInfrastructureConfiguration maps an SDK infrastructure configuration into
// the scanner-owned model. The EC2 key pair name is reduced to a configured
// boolean; instance user data is never present in this control-plane response.
func mapInfrastructureConfiguration(
	config awsimagebuildertypes.InfrastructureConfiguration,
) imagebuilderservice.InfrastructureConfiguration {
	mapped := imagebuilderservice.InfrastructureConfiguration{
		ARN:                        strings.TrimSpace(aws.ToString(config.Arn)),
		Name:                       strings.TrimSpace(aws.ToString(config.Name)),
		Description:                strings.TrimSpace(aws.ToString(config.Description)),
		InstanceProfileName:        strings.TrimSpace(aws.ToString(config.InstanceProfileName)),
		InstanceTypes:              trimStrings(config.InstanceTypes),
		KeyPairConfigured:          strings.TrimSpace(aws.ToString(config.KeyPair)) != "",
		SubnetID:                   strings.TrimSpace(aws.ToString(config.SubnetId)),
		SecurityGroupIDs:           trimStrings(config.SecurityGroupIds),
		SNSTopicARN:                strings.TrimSpace(aws.ToString(config.SnsTopicArn)),
		TerminateInstanceOnFailure: aws.ToBool(config.TerminateInstanceOnFailure),
		DateCreated:                strings.TrimSpace(aws.ToString(config.DateCreated)),
		DateUpdated:                strings.TrimSpace(aws.ToString(config.DateUpdated)),
		Tags:                       config.Tags,
	}
	if logging := config.Logging; logging != nil && logging.S3Logs != nil {
		mapped.LoggingS3BucketName = strings.TrimSpace(aws.ToString(logging.S3Logs.S3BucketName))
		mapped.LoggingS3KeyPrefix = strings.TrimSpace(aws.ToString(logging.S3Logs.S3KeyPrefix))
	}
	return mapped
}

// mapDistributionConfiguration maps an SDK distribution configuration into the
// scanner-owned model, recording only the distribution target regions and
// lifecycle metadata.
func mapDistributionConfiguration(
	config awsimagebuildertypes.DistributionConfiguration,
) imagebuilderservice.DistributionConfiguration {
	return imagebuilderservice.DistributionConfiguration{
		ARN:         strings.TrimSpace(aws.ToString(config.Arn)),
		Name:        strings.TrimSpace(aws.ToString(config.Name)),
		Description: strings.TrimSpace(aws.ToString(config.Description)),
		Regions:     distributionRegions(config.Distributions),
		DateCreated: strings.TrimSpace(aws.ToString(config.DateCreated)),
		DateUpdated: strings.TrimSpace(aws.ToString(config.DateUpdated)),
		Tags:        config.Tags,
	}
}

// componentARNs extracts the ordered component ARN references from a recipe's
// component configurations. The component build-document body is never fetched.
func componentARNs(components []awsimagebuildertypes.ComponentConfiguration) []string {
	if len(components) == 0 {
		return nil
	}
	arns := make([]string, 0, len(components))
	for i := range components {
		if arn := strings.TrimSpace(aws.ToString(components[i].ComponentArn)); arn != "" {
			arns = append(arns, arn)
		}
	}
	if len(arns) == 0 {
		return nil
	}
	return arns
}

// distributionRegions extracts the ordered, de-duplicated target regions from a
// distribution configuration's per-region distributions.
func distributionRegions(distributions []awsimagebuildertypes.Distribution) []string {
	if len(distributions) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(distributions))
	regions := make([]string, 0, len(distributions))
	for i := range distributions {
		region := strings.TrimSpace(aws.ToString(distributions[i].Region))
		if region == "" {
			continue
		}
		if _, ok := seen[region]; ok {
			continue
		}
		seen[region] = struct{}{}
		regions = append(regions, region)
	}
	if len(regions) == 0 {
		return nil
	}
	return regions
}

// trimStrings returns a trimmed copy of input with empty entries dropped, or nil
// when nothing survives.
func trimStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
