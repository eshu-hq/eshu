// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// containerImageTargetType labels a container image URI relationship target.
// SageMaker model containers reference image URIs that may live in ECR or any
// OCI registry, so the target is a generic image reference rather than an
// ECR-specific resource type.
const containerImageTargetType = "container_image"

func notebookRelationships(notebook NotebookInstance) []awscloud.RelationshipObservation {
	id := firstNonEmpty(notebook.ARN, notebook.Name)
	subnet := strings.TrimSpace(notebook.SubnetID)
	if id == "" || subnet == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		RelationshipType: awscloud.RelationshipSageMakerNotebookInstanceUsesSubnet,
		SourceResourceID: id,
		SourceARN:        strings.TrimSpace(notebook.ARN),
		TargetResourceID: subnet,
		TargetType:       awscloud.ResourceTypeEC2Subnet,
		SourceRecordID:   id + "#subnet#" + subnet,
	}}
}

func modelRelationships(model Model) []awscloud.RelationshipObservation {
	id := firstNonEmpty(model.ARN, model.Name)
	if id == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if role := strings.TrimSpace(model.ExecutionRole); isARN(role) {
		observations = append(observations, awscloud.RelationshipObservation{
			RelationshipType: awscloud.RelationshipSageMakerModelUsesIAMRole,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(model.ARN),
			TargetResourceID: role,
			TargetARN:        role,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   id + "#role#" + role,
		})
	}
	seenImage := make(map[string]struct{}, len(model.Containers))
	seenArtifact := make(map[string]struct{}, len(model.Containers))
	for _, container := range model.Containers {
		if image := strings.TrimSpace(container.Image); image != "" {
			if _, ok := seenImage[image]; !ok {
				seenImage[image] = struct{}{}
				observations = append(observations, awscloud.RelationshipObservation{
					RelationshipType: awscloud.RelationshipSageMakerModelUsesContainerImage,
					SourceResourceID: id,
					SourceARN:        strings.TrimSpace(model.ARN),
					TargetResourceID: image,
					TargetType:       containerImageTargetType,
					SourceRecordID:   id + "#image#" + image,
				})
			}
		}
		if observation, ok := modelArtifactRelationship(id, model.ARN, container.ModelDataURL, seenArtifact); ok {
			observations = append(observations, observation)
		}
	}
	return observations
}

// modelArtifactRelationship maps an `s3://bucket/key` model-artifact URL to an
// S3 bucket relationship target, keeping the raw URL and key prefix as
// attributes. It returns ok=false for blank, non-S3, or duplicate URLs.
func modelArtifactRelationship(
	modelID, modelARN, modelDataURL string,
	seen map[string]struct{},
) (awscloud.RelationshipObservation, bool) {
	artifact := strings.TrimSpace(modelDataURL)
	if artifact == "" {
		return awscloud.RelationshipObservation{}, false
	}
	if _, ok := seen[artifact]; ok {
		return awscloud.RelationshipObservation{}, false
	}
	seen[artifact] = struct{}{}
	bucket, key, ok := parseS3URL(artifact)
	if !ok {
		return awscloud.RelationshipObservation{}, false
	}
	bucketARN := "arn:" + awscloud.PartitionFromARN(modelARN) + ":s3:::" + bucket
	attributes := map[string]any{"model_data_url": artifact, "bucket": bucket}
	if key != "" {
		attributes["object_key"] = key
	}
	return awscloud.RelationshipObservation{
		RelationshipType: awscloud.RelationshipSageMakerModelUsesS3Artifact,
		SourceResourceID: modelID,
		SourceARN:        strings.TrimSpace(modelARN),
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   modelID + "#artifact#" + artifact,
	}, true
}

// parseS3URL splits an `s3://bucket[/key]` URL into the bucket name and
// optional object key. It returns ok=false when the input is not an `s3://`
// URL or carries no bucket segment.
func parseS3URL(url string) (bucket string, key string, ok bool) {
	trimmed := strings.TrimSpace(url)
	if !strings.HasPrefix(trimmed, "s3://") {
		return "", "", false
	}
	remainder := strings.TrimPrefix(trimmed, "s3://")
	if remainder == "" {
		return "", "", false
	}
	bucket, key, _ = strings.Cut(remainder, "/")
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return "", "", false
	}
	return bucket, strings.TrimSpace(key), true
}

func endpointRelationships(endpoint Endpoint) []awscloud.RelationshipObservation {
	id := firstNonEmpty(endpoint.ARN, endpoint.Name)
	config := strings.TrimSpace(endpoint.EndpointConfig)
	if id == "" || config == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		RelationshipType: awscloud.RelationshipSageMakerEndpointUsesEndpointConfig,
		SourceResourceID: id,
		SourceARN:        strings.TrimSpace(endpoint.ARN),
		TargetResourceID: config,
		TargetType:       awscloud.ResourceTypeSageMakerEndpointConfig,
		SourceRecordID:   id + "#endpoint-config#" + config,
	}}
}

func endpointConfigRelationships(config EndpointConfig) []awscloud.RelationshipObservation {
	id := firstNonEmpty(config.ARN, config.Name)
	if id == "" {
		return nil
	}
	seen := make(map[string]struct{}, len(config.ModelNames))
	var observations []awscloud.RelationshipObservation
	for _, model := range config.ModelNames {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			RelationshipType: awscloud.RelationshipSageMakerEndpointConfigUsesModel,
			SourceResourceID: id,
			SourceARN:        strings.TrimSpace(config.ARN),
			TargetResourceID: model,
			TargetType:       awscloud.ResourceTypeSageMakerModel,
			SourceRecordID:   id + "#model#" + model,
		})
	}
	return observations
}

func trainingJobRelationships(job TrainingJob) []awscloud.RelationshipObservation {
	id := firstNonEmpty(job.ARN, job.Name)
	role := strings.TrimSpace(job.ExecutionRole)
	if id == "" || !isARN(role) {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		RelationshipType: awscloud.RelationshipSageMakerTrainingJobUsesIAMRole,
		SourceResourceID: id,
		SourceARN:        strings.TrimSpace(job.ARN),
		TargetResourceID: role,
		TargetARN:        role,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   id + "#role#" + role,
	}}
}

func domainRelationships(domain Domain) []awscloud.RelationshipObservation {
	id := firstNonEmpty(domain.ARN, domain.ID, domain.Name)
	vpc := strings.TrimSpace(domain.VPCID)
	if id == "" || vpc == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		RelationshipType: awscloud.RelationshipSageMakerDomainUsesVPC,
		SourceResourceID: id,
		SourceARN:        strings.TrimSpace(domain.ARN),
		TargetResourceID: vpc,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		SourceRecordID:   id + "#vpc#" + vpc,
	}}
}

func userProfileRelationships(profile UserProfile) []awscloud.RelationshipObservation {
	name := strings.TrimSpace(profile.Name)
	domainID := strings.TrimSpace(profile.DomainID)
	id := firstNonEmpty(userProfileID(domainID, name), name)
	if id == "" || domainID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		RelationshipType: awscloud.RelationshipSageMakerUserProfileInDomain,
		SourceResourceID: id,
		TargetResourceID: domainID,
		TargetType:       awscloud.ResourceTypeSageMakerDomain,
		SourceRecordID:   id + "#domain#" + domainID,
	}}
}
