// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// pipelineObservation maps an image pipeline into its resource node. The
// resource_id is the pipeline ARN, which is the value pipeline edges source on.
func pipelineObservation(boundary awscloud.Boundary, pipeline ImagePipeline) awscloud.ResourceObservation {
	arn := trimSpace(pipeline.ARN)
	name := trimSpace(pipeline.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeImageBuilderImagePipeline,
		Name:         name,
		State:        trimSpace(pipeline.Status),
		Tags:         cloneStringMap(pipeline.Tags),
		Attributes: map[string]any{
			"description":                      trimSpace(pipeline.Description),
			"platform":                         trimSpace(pipeline.Platform),
			"image_recipe_arn":                 trimSpace(pipeline.ImageRecipeARN),
			"container_recipe_arn":             trimSpace(pipeline.ContainerRecipeARN),
			"infrastructure_configuration_arn": trimSpace(pipeline.InfrastructureConfigurationARN),
			"distribution_configuration_arn":   trimSpace(pipeline.DistributionConfigurationARN),
			"execution_role_arn":               trimSpace(pipeline.ExecutionRoleARN),
			"enhanced_image_metadata_enabled":  pipeline.EnhancedImageMetadataEnabled,
			"schedule_expression":              trimSpace(pipeline.ScheduleExpression),
			"date_created":                     parsedTimeOrNil(pipeline.DateCreated),
			"date_updated":                     parsedTimeOrNil(pipeline.DateUpdated),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// imageRecipeObservation maps an image recipe into its resource node. The parent
// image is recorded as an attribute because there is no EC2 AMI resource type to
// key an edge to; component ARNs are recorded as references only, never bodies.
func imageRecipeObservation(boundary awscloud.Boundary, recipe ImageRecipe) awscloud.ResourceObservation {
	arn := trimSpace(recipe.ARN)
	name := trimSpace(recipe.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeImageBuilderImageRecipe,
		Name:         name,
		Tags:         cloneStringMap(recipe.Tags),
		Attributes: map[string]any{
			"description":       trimSpace(recipe.Description),
			"platform":          trimSpace(recipe.Platform),
			"version":           trimSpace(recipe.Version),
			"owner":             trimSpace(recipe.Owner),
			"parent_image":      trimSpace(recipe.ParentImage),
			"working_directory": trimSpace(recipe.WorkingDirectory),
			"component_arns":    cloneStrings(recipe.ComponentARNs),
			"component_count":   len(cloneStrings(recipe.ComponentARNs)),
			"date_created":      parsedTimeOrNil(recipe.DateCreated),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// containerRecipeObservation maps a container recipe into its resource node. The
// Dockerfile template body is never read; the target ECR repository name and KMS
// key reference are kept for edges, and the parent image is an attribute.
func containerRecipeObservation(boundary awscloud.Boundary, recipe ContainerRecipe) awscloud.ResourceObservation {
	arn := trimSpace(recipe.ARN)
	name := trimSpace(recipe.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeImageBuilderContainerRecipe,
		Name:         name,
		Tags:         cloneStringMap(recipe.Tags),
		Attributes: map[string]any{
			"description":               trimSpace(recipe.Description),
			"platform":                  trimSpace(recipe.Platform),
			"version":                   trimSpace(recipe.Version),
			"owner":                     trimSpace(recipe.Owner),
			"container_type":            trimSpace(recipe.ContainerType),
			"parent_image":              trimSpace(recipe.ParentImage),
			"working_directory":         trimSpace(recipe.WorkingDirectory),
			"target_repository_name":    trimSpace(recipe.TargetRepositoryName),
			"target_repository_service": trimSpace(recipe.TargetRepositoryService),
			"kms_key_id":                trimSpace(recipe.KMSKeyID),
			"encrypted":                 recipe.Encrypted,
			"component_arns":            cloneStrings(recipe.ComponentARNs),
			"component_count":           len(cloneStrings(recipe.ComponentARNs)),
			"date_created":              parsedTimeOrNil(recipe.DateCreated),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// infraConfigObservation maps an infrastructure configuration into its resource
// node. The instance profile name, networking references, SNS topic, and S3
// logging location are kept for edges; no instance user data is read.
func infraConfigObservation(boundary awscloud.Boundary, config InfrastructureConfiguration) awscloud.ResourceObservation {
	arn := trimSpace(config.ARN)
	name := trimSpace(config.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeImageBuilderInfrastructureConfiguration,
		Name:         name,
		Tags:         cloneStringMap(config.Tags),
		Attributes: map[string]any{
			"description":                   trimSpace(config.Description),
			"instance_profile_name":         trimSpace(config.InstanceProfileName),
			"instance_types":                cloneStrings(config.InstanceTypes),
			"key_pair_configured":           config.KeyPairConfigured,
			"subnet_id":                     trimSpace(config.SubnetID),
			"security_group_ids":            cloneStrings(config.SecurityGroupIDs),
			"sns_topic_arn":                 trimSpace(config.SNSTopicARN),
			"logging_s3_bucket_name":        trimSpace(config.LoggingS3BucketName),
			"logging_s3_key_prefix":         trimSpace(config.LoggingS3KeyPrefix),
			"terminate_instance_on_failure": config.TerminateInstanceOnFailure,
			"date_created":                  parsedTimeOrNil(config.DateCreated),
			"date_updated":                  parsedTimeOrNil(config.DateUpdated),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// distributionConfigObservation maps a distribution configuration into its
// resource node. Only the distribution target regions and lifecycle metadata are
// recorded; per-region AMI and container distribution settings stay out of the
// fact payload.
func distributionConfigObservation(boundary awscloud.Boundary, config DistributionConfiguration) awscloud.ResourceObservation {
	arn := trimSpace(config.ARN)
	name := trimSpace(config.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   arn,
		ResourceType: awscloud.ResourceTypeImageBuilderDistributionConfiguration,
		Name:         name,
		Tags:         cloneStringMap(config.Tags),
		Attributes: map[string]any{
			"description":  trimSpace(config.Description),
			"regions":      cloneStrings(config.Regions),
			"region_count": len(cloneStrings(config.Regions)),
			"date_created": parsedTimeOrNil(config.DateCreated),
			"date_updated": parsedTimeOrNil(config.DateUpdated),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     arn,
	}
}

// parsedTimeOrNil parses an AWS-reported timestamp string and returns the UTC
// time, or nil when the string is blank or unparseable, so the attribute payload
// omits an unknown timestamp instead of emitting a zero value.
func parsedTimeOrNil(value string) any {
	return timeOrNil(parsedTime(value))
}
