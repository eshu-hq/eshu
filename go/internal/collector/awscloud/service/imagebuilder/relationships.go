// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagebuilder

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// pipelineRelationships returns every cross-resource edge an image pipeline
// reports: the image or container recipe it builds from, the infrastructure
// configuration it runs on, the distribution configuration it distributes
// through, and the IAM execution role it assumes. Each ARN-keyed edge joins the
// node the target scanner publishes by the same ARN. Edges with a missing
// endpoint are skipped, never dangled.
func pipelineRelationships(boundary awscloud.Boundary, pipeline ImagePipeline) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(pipeline.ARN)
	if sourceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	add := func(relType, targetID, targetType string) {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			return
		}
		observations = append(observations, arnEdge(boundary, sourceID, relType, targetID, targetType))
	}
	add(awscloud.RelationshipImageBuilderPipelineUsesImageRecipe, pipeline.ImageRecipeARN, awscloud.ResourceTypeImageBuilderImageRecipe)
	add(awscloud.RelationshipImageBuilderPipelineUsesContainerRecipe, pipeline.ContainerRecipeARN, awscloud.ResourceTypeImageBuilderContainerRecipe)
	add(
		awscloud.RelationshipImageBuilderPipelineUsesInfrastructureConfiguration,
		pipeline.InfrastructureConfigurationARN,
		awscloud.ResourceTypeImageBuilderInfrastructureConfiguration,
	)
	add(
		awscloud.RelationshipImageBuilderPipelineUsesDistributionConfiguration,
		pipeline.DistributionConfigurationARN,
		awscloud.ResourceTypeImageBuilderDistributionConfiguration,
	)
	add(awscloud.RelationshipImageBuilderPipelineUsesExecutionRole, pipeline.ExecutionRoleARN, awscloud.ResourceTypeIAMRole)
	return observations
}

// infraConfigRelationships returns every cross-resource edge an infrastructure
// configuration reports: the IAM instance profile, build subnet, security
// groups, build-status SNS topic, and build-log S3 bucket. Name-keyed targets
// are synthesized into the partition-aware ARN the target scanner publishes;
// id-keyed targets (subnet, security group) are kept bare. Edges with a missing
// or unresolvable endpoint are skipped, never dangled.
func infraConfigRelationships(boundary awscloud.Boundary, config InfrastructureConfiguration) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(config.ARN)
	if sourceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	partition := awscloud.PartitionForBoundary(boundary)

	if profileARN := arnForInstanceProfile(partition, boundary.AccountID, config.InstanceProfileName); profileARN != "" {
		observations = append(observations, arnEdge(
			boundary, sourceID,
			awscloud.RelationshipImageBuilderInfraConfigUsesInstanceProfile,
			profileARN, awscloud.ResourceTypeIAMInstanceProfile,
		))
	}
	if subnetID := strings.TrimSpace(config.SubnetID); subnetID != "" {
		observations = append(observations, bareEdge(
			boundary, sourceID,
			awscloud.RelationshipImageBuilderInfraConfigUsesSubnet,
			subnetID, awscloud.ResourceTypeEC2Subnet,
		))
	}
	for _, groupID := range cloneStrings(config.SecurityGroupIDs) {
		observations = append(observations, bareEdge(
			boundary, sourceID,
			awscloud.RelationshipImageBuilderInfraConfigUsesSecurityGroup,
			groupID, awscloud.ResourceTypeEC2SecurityGroup,
		))
	}
	if topicARN := strings.TrimSpace(config.SNSTopicARN); topicARN != "" {
		observations = append(observations, arnEdge(
			boundary, sourceID,
			awscloud.RelationshipImageBuilderInfraConfigUsesSNSTopic,
			topicARN, awscloud.ResourceTypeSNSTopic,
		))
	}
	if edge := infraConfigS3LoggingEdge(boundary, partition, sourceID, config); edge != nil {
		observations = append(observations, *edge)
	}
	return observations
}

// infraConfigS3LoggingEdge builds the infrastructure-configuration->S3 build-log
// bucket edge. AWS reports the bucket NAME, so the scanner synthesizes the
// partition-aware bucket ARN the S3 scanner publishes and carries the optional
// key prefix as an attribute. It returns nil when no logging bucket is set.
func infraConfigS3LoggingEdge(
	boundary awscloud.Boundary,
	partition, sourceID string,
	config InfrastructureConfiguration,
) *awscloud.RelationshipObservation {
	bucket := strings.TrimSpace(config.LoggingS3BucketName)
	if bucket == "" {
		return nil
	}
	bucketARN := arnForBucket(partition, bucket)
	if bucketARN == "" {
		return nil
	}
	attributes := map[string]any{"bucket": bucket}
	if prefix := strings.TrimSpace(config.LoggingS3KeyPrefix); prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	edge := arnEdge(
		boundary, sourceID,
		awscloud.RelationshipImageBuilderInfraConfigLogsToS3,
		bucketARN, awscloud.ResourceTypeS3Bucket,
	)
	edge.Attributes = attributes
	return &edge
}

// containerRecipeRelationships returns the container recipe's target-ECR
// repository and KMS encryption key edges. The ECR target is the partition-aware
// repository ARN synthesized from the reported repository NAME; the KMS target
// is keyed by the reported key identifier with target_arn set only when
// ARN-shaped. Edges with a missing or unresolvable endpoint are skipped.
func containerRecipeRelationships(boundary awscloud.Boundary, recipe ContainerRecipe) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(recipe.ARN)
	if sourceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if edge := containerRecipeECREdge(boundary, sourceID, recipe); edge != nil {
		observations = append(observations, *edge)
	}
	if edge := containerRecipeKMSEdge(boundary, sourceID, recipe); edge != nil {
		observations = append(observations, *edge)
	}
	return observations
}

// containerRecipeECREdge builds the container-recipe->ECR repository edge when
// the recipe targets an ECR repository. AWS reports a bare repository NAME, so
// the scanner synthesizes the partition-aware repository ARN the ECR scanner
// publishes. It returns nil when the target is not ECR or the repository ARN is
// unresolvable.
func containerRecipeECREdge(
	boundary awscloud.Boundary,
	sourceID string,
	recipe ContainerRecipe,
) *awscloud.RelationshipObservation {
	name := strings.TrimSpace(recipe.TargetRepositoryName)
	if name == "" {
		return nil
	}
	if service := strings.TrimSpace(recipe.TargetRepositoryService); service != "" && !strings.EqualFold(service, "ECR") {
		return nil
	}
	repoARN := arnForECRRepository(
		awscloud.PartitionForBoundary(boundary),
		boundary.Region,
		boundary.AccountID,
		name,
	)
	if repoARN == "" {
		return nil
	}
	edge := arnEdge(
		boundary, sourceID,
		awscloud.RelationshipImageBuilderContainerRecipeUsesECRRepository,
		repoARN, awscloud.ResourceTypeECRRepository,
	)
	edge.Attributes = map[string]any{"repository_name": name}
	return &edge
}

// containerRecipeKMSEdge builds the container-recipe->KMS key edge from the
// reported encryption key identifier. AWS may report a key id, key ARN, or
// alias; the value is the join key the KMS scanner publishes, and target_arn is
// set only for ARN-shaped identifiers so a bare id or alias is never given a
// fabricated ARN. It returns nil when no key is reported.
func containerRecipeKMSEdge(
	boundary awscloud.Boundary,
	sourceID string,
	recipe ContainerRecipe,
) *awscloud.RelationshipObservation {
	keyID := strings.TrimSpace(recipe.KMSKeyID)
	if keyID == "" {
		return nil
	}
	edge := awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipImageBuilderContainerRecipeUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		TargetResourceID: keyID,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipImageBuilderContainerRecipeUsesKMSKey + ":" + keyID,
	}
	if isARN(keyID) {
		edge.TargetARN = keyID
	}
	return &edge
}

// arnEdge builds a relationship observation whose target is keyed by an ARN. The
// target_arn is set to the same ARN so the runtime graph-join guard sees a
// consistent ARN-keyed edge.
func arnEdge(boundary awscloud.Boundary, sourceID, relType, targetID, targetType string) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relType,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetID,
		TargetType:       targetType,
		SourceRecordID:   sourceID + "->" + relType + ":" + targetID,
	}
}

// bareEdge builds a relationship observation whose target is keyed by a bare AWS
// id (subnet-..., sg-...), never an ARN. target_arn is left empty so the runtime
// graph-join guard does not flag a bare id against a populated ARN.
func bareEdge(boundary awscloud.Boundary, sourceID, relType, targetID, targetType string) awscloud.RelationshipObservation {
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relType,
		SourceResourceID: sourceID,
		SourceARN:        sourceID,
		TargetResourceID: targetID,
		TargetType:       targetType,
		SourceRecordID:   sourceID + "->" + relType + ":" + targetID,
	}
}
