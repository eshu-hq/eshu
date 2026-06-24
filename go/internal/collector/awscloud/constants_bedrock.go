// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
