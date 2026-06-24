// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSageMaker identifies the regional Amazon SageMaker metadata-only
	// scan slice. It covers the SageMaker control plane: notebook instances,
	// endpoints, endpoint configurations, models, training/processing/transform
	// jobs, hyperparameter tuning jobs, projects, pipelines, feature groups,
	// Studio domains, user profiles, apps, and inference components. The scanner
	// never invokes endpoints, never runs inference, and never persists
	// hyperparameter values, training input/output data contents, notebook
	// lifecycle-config script bodies, or pipeline definition bodies.
	ServiceSageMaker = "sagemaker"
)

const (
	// ResourceTypeSageMakerNotebookInstance identifies a SageMaker notebook
	// instance metadata resource.
	ResourceTypeSageMakerNotebookInstance = "aws_sagemaker_notebook_instance"
	// ResourceTypeSageMakerEndpoint identifies a SageMaker endpoint metadata
	// resource.
	ResourceTypeSageMakerEndpoint = "aws_sagemaker_endpoint"
	// ResourceTypeSageMakerEndpointConfig identifies a SageMaker endpoint
	// configuration metadata resource.
	ResourceTypeSageMakerEndpointConfig = "aws_sagemaker_endpoint_config"
	// ResourceTypeSageMakerModel identifies a SageMaker model metadata resource.
	ResourceTypeSageMakerModel = "aws_sagemaker_model"
	// ResourceTypeSageMakerTrainingJob identifies a SageMaker training job
	// metadata resource. Hyperparameter values and training data contents are
	// never persisted.
	ResourceTypeSageMakerTrainingJob = "aws_sagemaker_training_job"
	// ResourceTypeSageMakerProcessingJob identifies a SageMaker processing job
	// metadata resource.
	ResourceTypeSageMakerProcessingJob = "aws_sagemaker_processing_job"
	// ResourceTypeSageMakerTransformJob identifies a SageMaker batch transform
	// job metadata resource.
	ResourceTypeSageMakerTransformJob = "aws_sagemaker_transform_job"
	// ResourceTypeSageMakerHyperParameterTuningJob identifies a SageMaker
	// hyperparameter tuning job metadata resource. Tuned hyperparameter values
	// are never persisted.
	ResourceTypeSageMakerHyperParameterTuningJob = "aws_sagemaker_hyperparameter_tuning_job"
	// ResourceTypeSageMakerProject identifies a SageMaker project metadata
	// resource.
	ResourceTypeSageMakerProject = "aws_sagemaker_project"
	// ResourceTypeSageMakerPipeline identifies a SageMaker pipeline metadata
	// resource. The pipeline definition body is never persisted.
	ResourceTypeSageMakerPipeline = "aws_sagemaker_pipeline"
	// ResourceTypeSageMakerFeatureGroup identifies a SageMaker feature group
	// metadata resource. Feature record contents are never read or persisted.
	ResourceTypeSageMakerFeatureGroup = "aws_sagemaker_feature_group"
	// ResourceTypeSageMakerDomain identifies a SageMaker Studio domain metadata
	// resource.
	ResourceTypeSageMakerDomain = "aws_sagemaker_domain"
	// ResourceTypeSageMakerUserProfile identifies a SageMaker Studio user
	// profile metadata resource.
	ResourceTypeSageMakerUserProfile = "aws_sagemaker_user_profile"
	// ResourceTypeSageMakerApp identifies a SageMaker Studio app metadata
	// resource.
	ResourceTypeSageMakerApp = "aws_sagemaker_app"
	// ResourceTypeSageMakerInferenceComponent identifies a SageMaker inference
	// component metadata resource.
	ResourceTypeSageMakerInferenceComponent = "aws_sagemaker_inference_component"
)

const (
	// RelationshipSageMakerModelUsesS3Artifact records a model's reported S3
	// model-artifact location when AWS reports a ModelDataUrl.
	RelationshipSageMakerModelUsesS3Artifact = "sagemaker_model_uses_s3_artifact"
	// RelationshipSageMakerModelUsesContainerImage records a model's reported
	// container image URI dependency.
	RelationshipSageMakerModelUsesContainerImage = "sagemaker_model_uses_container_image"
	// RelationshipSageMakerModelUsesIAMRole records a model's reported execution
	// IAM role dependency.
	RelationshipSageMakerModelUsesIAMRole = "sagemaker_model_uses_iam_role"
	// RelationshipSageMakerEndpointUsesEndpointConfig records an endpoint's
	// reported active endpoint configuration.
	RelationshipSageMakerEndpointUsesEndpointConfig = "sagemaker_endpoint_uses_endpoint_config"
	// RelationshipSageMakerEndpointConfigUsesModel records an endpoint
	// configuration's reported production-variant model dependency.
	RelationshipSageMakerEndpointConfigUsesModel = "sagemaker_endpoint_config_uses_model"
	// RelationshipSageMakerTrainingJobUsesIAMRole records a training job's
	// reported execution IAM role dependency.
	RelationshipSageMakerTrainingJobUsesIAMRole = "sagemaker_training_job_uses_iam_role"
	// RelationshipSageMakerNotebookInstanceUsesSubnet records a notebook
	// instance's reported VPC subnet placement.
	RelationshipSageMakerNotebookInstanceUsesSubnet = "sagemaker_notebook_instance_uses_subnet"
	// RelationshipSageMakerDomainUsesVPC records a Studio domain's reported VPC
	// placement.
	RelationshipSageMakerDomainUsesVPC = "sagemaker_domain_uses_vpc"
	// RelationshipSageMakerUserProfileInDomain records a Studio user profile's
	// reported parent domain.
	RelationshipSageMakerUserProfileInDomain = "sagemaker_user_profile_in_domain"
)
