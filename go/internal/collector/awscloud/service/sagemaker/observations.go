// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func notebookObservation(notebook NotebookInstance) awscloud.ResourceObservation {
	arn := strings.TrimSpace(notebook.ARN)
	id := firstNonEmpty(arn, notebook.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerNotebookInstance,
		Name:         strings.TrimSpace(notebook.Name),
		State:        strings.TrimSpace(notebook.Status),
		Tags:         cloneStringMap(notebook.Tags),
		Attributes: map[string]any{
			"instance_type":          strings.TrimSpace(notebook.InstanceType),
			"subnet_id":              strings.TrimSpace(notebook.SubnetID),
			"security_group_ids":     cloneStrings(notebook.SecurityGroupIDs),
			"direct_internet_access": strings.TrimSpace(notebook.DirectInternetAccess),
			"lifecycle_config_name":  strings.TrimSpace(notebook.LifecycleConfigName),
			"platform_identifier":    strings.TrimSpace(notebook.PlatformIdentifier),
			"creation_time":          timeOrNil(notebook.CreationTime),
			"last_modified_time":     timeOrNil(notebook.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, notebook.Name},
		SourceRecordID:     id,
	}
}

func modelObservation(model Model) awscloud.ResourceObservation {
	arn := strings.TrimSpace(model.ARN)
	id := firstNonEmpty(arn, model.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerModel,
		Name:         strings.TrimSpace(model.Name),
		Tags:         cloneStringMap(model.Tags),
		Attributes: map[string]any{
			"execution_role_arn":   strings.TrimSpace(model.ExecutionRole),
			"network_isolated":     model.NetworkIsolated,
			"vpc_subnet_ids":       cloneStrings(model.VPCSubnetIDs),
			"container_image_uris": containerImageURIs(model.Containers),
			"model_data_urls":      modelArtifactURLs(model.Containers),
			"container_count":      len(model.Containers),
			"creation_time":        timeOrNil(model.CreationTime),
		},
		CorrelationAnchors: []string{arn, model.Name},
		SourceRecordID:     id,
	}
}

func endpointObservation(endpoint Endpoint) awscloud.ResourceObservation {
	arn := strings.TrimSpace(endpoint.ARN)
	id := firstNonEmpty(arn, endpoint.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerEndpoint,
		Name:         strings.TrimSpace(endpoint.Name),
		State:        strings.TrimSpace(endpoint.Status),
		Tags:         cloneStringMap(endpoint.Tags),
		Attributes: map[string]any{
			"endpoint_config_name": strings.TrimSpace(endpoint.EndpointConfig),
			"creation_time":        timeOrNil(endpoint.CreationTime),
			"last_modified_time":   timeOrNil(endpoint.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, endpoint.Name},
		SourceRecordID:     id,
	}
}

func endpointConfigObservation(config EndpointConfig) awscloud.ResourceObservation {
	arn := strings.TrimSpace(config.ARN)
	id := firstNonEmpty(arn, config.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerEndpointConfig,
		Name:         strings.TrimSpace(config.Name),
		Tags:         cloneStringMap(config.Tags),
		Attributes: map[string]any{
			"kms_key_id":    strings.TrimSpace(config.KMSKeyID),
			"model_names":   cloneStrings(config.ModelNames),
			"variant_count": len(config.ModelNames),
			"creation_time": timeOrNil(config.CreationTime),
		},
		CorrelationAnchors: []string{arn, config.Name},
		SourceRecordID:     id,
	}
}

func trainingJobObservation(job TrainingJob) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	id := firstNonEmpty(arn, job.Name)
	// HyperParameters and training input/output data references are
	// intentionally omitted: values may be secret-like and data references can
	// leak training-set contents.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerTrainingJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"execution_role_arn": strings.TrimSpace(job.ExecutionRole),
			"secondary_status":   strings.TrimSpace(job.SecondaryStatus),
			"creation_time":      timeOrNil(job.CreationTime),
			"last_modified_time": timeOrNil(job.LastModifiedTime),
			"training_end_time":  timeOrNil(job.TrainingEndTime),
		},
		CorrelationAnchors: []string{arn, job.Name},
		SourceRecordID:     id,
	}
}

func processingJobObservation(job ProcessingJob) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	id := firstNonEmpty(arn, job.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerProcessingJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"creation_time":       timeOrNil(job.CreationTime),
			"last_modified_time":  timeOrNil(job.LastModifiedTime),
			"processing_end_time": timeOrNil(job.ProcessingEnd),
		},
		CorrelationAnchors: []string{arn, job.Name},
		SourceRecordID:     id,
	}
}

func transformJobObservation(job TransformJob) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	id := firstNonEmpty(arn, job.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerTransformJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"creation_time":      timeOrNil(job.CreationTime),
			"last_modified_time": timeOrNil(job.LastModifiedTime),
			"transform_end_time": timeOrNil(job.TransformEnd),
		},
		CorrelationAnchors: []string{arn, job.Name},
		SourceRecordID:     id,
	}
}

func tuningJobObservation(job HyperParameterTuningJob) awscloud.ResourceObservation {
	arn := strings.TrimSpace(job.ARN)
	id := firstNonEmpty(arn, job.Name)
	// Tuned hyperparameter ranges and values are not persisted; only the
	// bounded strategy label and status describe the tuning job.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerHyperParameterTuningJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Tags:         cloneStringMap(job.Tags),
		Attributes: map[string]any{
			"strategy":           strings.TrimSpace(job.Strategy),
			"creation_time":      timeOrNil(job.CreationTime),
			"last_modified_time": timeOrNil(job.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, job.Name},
		SourceRecordID:     id,
	}
}

func projectObservation(project Project) awscloud.ResourceObservation {
	arn := strings.TrimSpace(project.ARN)
	id := firstNonEmpty(arn, project.ID, project.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerProject,
		Name:         strings.TrimSpace(project.Name),
		State:        strings.TrimSpace(project.Status),
		Tags:         cloneStringMap(project.Tags),
		Attributes: map[string]any{
			"project_id":    strings.TrimSpace(project.ID),
			"creation_time": timeOrNil(project.CreationTime),
		},
		CorrelationAnchors: []string{arn, project.ID, project.Name},
		SourceRecordID:     id,
	}
}

func pipelineObservation(pipeline Pipeline) awscloud.ResourceObservation {
	arn := strings.TrimSpace(pipeline.ARN)
	id := firstNonEmpty(arn, pipeline.Name)
	// The pipeline definition body is never read or persisted; its steps can
	// carry parameters with secret-like values.
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerPipeline,
		Name:         strings.TrimSpace(pipeline.Name),
		Tags:         cloneStringMap(pipeline.Tags),
		Attributes: map[string]any{
			"display_name":        strings.TrimSpace(pipeline.DisplayName),
			"creation_time":       timeOrNil(pipeline.CreationTime),
			"last_modified_time":  timeOrNil(pipeline.LastModifiedTime),
			"last_execution_time": timeOrNil(pipeline.LastExecution),
		},
		CorrelationAnchors: []string{arn, pipeline.Name},
		SourceRecordID:     id,
	}
}

func featureGroupObservation(group FeatureGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	id := firstNonEmpty(arn, group.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerFeatureGroup,
		Name:         strings.TrimSpace(group.Name),
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"offline_store_status": strings.TrimSpace(group.OfflineStore),
			"creation_time":        timeOrNil(group.CreationTime),
		},
		CorrelationAnchors: []string{arn, group.Name},
		SourceRecordID:     id,
	}
}

// containerImageURIs returns the inference container image URIs in pipeline
// order. Container Environment maps are never read into the model contract.
func containerImageURIs(containers []ModelContainer) []string {
	uris := make([]string, 0, len(containers))
	for _, container := range containers {
		if image := strings.TrimSpace(container.Image); image != "" {
			uris = append(uris, image)
		}
	}
	return cloneStrings(uris)
}

// modelArtifactURLs returns the S3 model-artifact locations in pipeline order.
func modelArtifactURLs(containers []ModelContainer) []string {
	urls := make([]string, 0, len(containers))
	for _, container := range containers {
		if url := strings.TrimSpace(container.ModelDataURL); url != "" {
			urls = append(urls, url)
		}
	}
	return cloneStrings(urls)
}
