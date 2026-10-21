// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Analytics-category constants consolidated from constants_sagemaker.go (#6696).

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

// Analytics-category constants consolidated from constants_bedrock.go (#6696).

const (
	// ServiceBedrock identifies the regional Amazon Bedrock metadata-only scan
	// slice. It covers the Bedrock control plane only: foundation model
	// availability, custom models, model customization jobs, provisioned model
	// throughputs, guardrails, agents, agent action groups, and knowledge
	// bases. The scanner never calls bedrock-runtime (InvokeModel, Converse) or
	// bedrock-agent-runtime (InvokeAgent, Retrieve, RetrieveAndGenerate); those
	// inference data-plane modules are never imported. It never persists agent
	// instructions, prompt-override configurations, guardrail topic or content
	// policy bodies, knowledge base ingested document content, or action-group
	// API schema bodies.
	ServiceBedrock = "bedrock"
)

const (
	// ResourceTypeBedrockFoundationModel identifies a Bedrock foundation model
	// availability metadata resource reported by the read-only model list.
	ResourceTypeBedrockFoundationModel = "aws_bedrock_foundation_model"
	// ResourceTypeBedrockCustomModel identifies a Bedrock custom model metadata
	// resource. The scanner persists the base model id, training job ARN, and
	// output S3 reference, never hyperparameter values or training input data.
	ResourceTypeBedrockCustomModel = "aws_bedrock_custom_model"
	// ResourceTypeBedrockModelCustomizationJob identifies a Bedrock model
	// customization job metadata resource.
	ResourceTypeBedrockModelCustomizationJob = "aws_bedrock_model_customization_job"
	// ResourceTypeBedrockProvisionedModelThroughput identifies a Bedrock
	// provisioned model throughput metadata resource.
	ResourceTypeBedrockProvisionedModelThroughput = "aws_bedrock_provisioned_model_throughput"
	// ResourceTypeBedrockGuardrail identifies a Bedrock guardrail metadata
	// resource. The scanner persists the name and status only; topic and content
	// policy bodies are never read or persisted.
	ResourceTypeBedrockGuardrail = "aws_bedrock_guardrail"
	// ResourceTypeBedrockAgent identifies a Bedrock agent metadata resource. The
	// scanner persists the name, description, and foundation model id only; agent
	// instructions and prompt-override configurations are never read or
	// persisted.
	ResourceTypeBedrockAgent = "aws_bedrock_agent"
	// ResourceTypeBedrockAgentActionGroup identifies a Bedrock agent action group
	// metadata resource. The scanner persists the name and Lambda executor ARN
	// only; the action-group API schema body is never read or persisted.
	ResourceTypeBedrockAgentActionGroup = "aws_bedrock_agent_action_group"
	// ResourceTypeBedrockKnowledgeBase identifies a Bedrock knowledge base
	// metadata resource. The scanner persists the name and embedding model
	// reference only; ingested document content and chunks are never read or
	// persisted.
	ResourceTypeBedrockKnowledgeBase = "aws_bedrock_knowledge_base"
)

const (
	// RelationshipBedrockCustomModelUsesBaseModel records a custom model's
	// reported base foundation model dependency.
	RelationshipBedrockCustomModelUsesBaseModel = "bedrock_custom_model_uses_base_model"
	// RelationshipBedrockCustomModelUsesS3Output records a custom model's reported
	// output S3 artifact location when AWS reports an output data S3 URI.
	RelationshipBedrockCustomModelUsesS3Output = "bedrock_custom_model_uses_s3_output"
	// RelationshipBedrockCustomModelFromCustomizationJob records the customization
	// job ARN that produced a custom model.
	RelationshipBedrockCustomModelFromCustomizationJob = "bedrock_custom_model_from_customization_job"
	// RelationshipBedrockProvisionedThroughputUsesModel records a provisioned
	// model throughput's reported model ARN dependency.
	RelationshipBedrockProvisionedThroughputUsesModel = "bedrock_provisioned_throughput_uses_model"
	// RelationshipBedrockAgentUsesFoundationModel records an agent's reported
	// foundation model id dependency.
	RelationshipBedrockAgentUsesFoundationModel = "bedrock_agent_uses_foundation_model"
	// RelationshipBedrockAgentUsesKnowledgeBase records an agent's reported
	// associated knowledge base.
	RelationshipBedrockAgentUsesKnowledgeBase = "bedrock_agent_uses_knowledge_base"
	// RelationshipBedrockAgentHasActionGroup records an agent's reported action
	// group membership.
	RelationshipBedrockAgentHasActionGroup = "bedrock_agent_has_action_group"
	// RelationshipBedrockActionGroupUsesLambda records an action group's reported
	// Lambda function executor.
	RelationshipBedrockActionGroupUsesLambda = "bedrock_action_group_uses_lambda"
	// RelationshipBedrockKnowledgeBaseUsesS3DataSource records a knowledge base's
	// reported S3 data source bucket.
	RelationshipBedrockKnowledgeBaseUsesS3DataSource = "bedrock_knowledge_base_uses_s3_data_source"
	// RelationshipBedrockKnowledgeBaseUsesConfluence records a knowledge base's
	// reported Confluence data source host URL.
	RelationshipBedrockKnowledgeBaseUsesConfluence = "bedrock_knowledge_base_uses_confluence"
	// RelationshipBedrockKnowledgeBaseUsesSharePoint records a knowledge base's
	// reported SharePoint data source site URL.
	RelationshipBedrockKnowledgeBaseUsesSharePoint = "bedrock_knowledge_base_uses_sharepoint"
	// RelationshipBedrockKnowledgeBaseUsesWebCrawler records a knowledge base's
	// reported web-crawler data source seed URL.
	RelationshipBedrockKnowledgeBaseUsesWebCrawler = "bedrock_knowledge_base_uses_web_crawler"
)
