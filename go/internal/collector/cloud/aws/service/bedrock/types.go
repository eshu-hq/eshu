// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"context"
	"time"
)

// Client is the Bedrock control-plane read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned
// metadata records. The interface deliberately exposes no inference call
// (no bedrock-runtime InvokeModel/Converse, no bedrock-agent-runtime
// InvokeAgent/Retrieve/RetrieveAndGenerate) and no mutation operation, so the
// scanner cannot run a model, query a knowledge base, or change Bedrock state
// through this contract.
type Client interface {
	ListFoundationModels(context.Context) ([]FoundationModel, error)
	ListCustomModels(context.Context) ([]CustomModel, error)
	ListModelCustomizationJobs(context.Context) ([]ModelCustomizationJob, error)
	ListProvisionedModelThroughputs(context.Context) ([]ProvisionedModelThroughput, error)
	ListGuardrails(context.Context) ([]Guardrail, error)
	ListAgents(context.Context) ([]Agent, error)
	ListAgentActionGroups(context.Context) ([]AgentActionGroup, error)
	ListKnowledgeBases(context.Context) ([]KnowledgeBase, error)
}

// FoundationModel is the scanner-owned representation of one Bedrock foundation
// model availability entry from the read-only model list. It carries the model
// id, ARN, provider, and lifecycle status only.
type FoundationModel struct {
	ARN             string
	ModelID         string
	ProviderName    string
	LifecycleStatus string
}

// CustomModel is the scanner-owned representation of one Bedrock custom model.
// It carries the base model id, training/customization job ARN, and the output
// S3 reference. Hyperparameter values and training input data references are
// never persisted: there is no field for them.
type CustomModel struct {
	ARN          string
	Name         string
	BaseModelARN string
	// JobARN is the customization/training job ARN that produced the model.
	JobARN string
	// OutputS3URI is the S3 URI of the custom model output artifact, when AWS
	// reports OutputDataConfig.
	OutputS3URI  string
	CreationTime time.Time
	Tags         map[string]string
}

// ModelCustomizationJob is the scanner-owned representation of one Bedrock
// model customization job. It carries job metadata and the base/custom model
// dependency only; hyperparameter values and training data are never persisted.
type ModelCustomizationJob struct {
	ARN            string
	Name           string
	Status         string
	BaseModelARN   string
	CustomModelARN string
	CreationTime   time.Time
	EndTime        time.Time
	Tags           map[string]string
}

// ProvisionedModelThroughput is the scanner-owned representation of one Bedrock
// provisioned model throughput. It carries the name, associated model ARN, and
// allocated model units.
type ProvisionedModelThroughput struct {
	ARN          string
	Name         string
	Status       string
	ModelARN     string
	ModelUnits   int32
	CreationTime time.Time
	Tags         map[string]string
}

// Guardrail is the scanner-owned representation of one Bedrock guardrail. It
// carries the name, id, version, and status only. Topic policy and content
// policy bodies encode an organization's content-safety posture and are never
// read or persisted: there is no field for them.
type Guardrail struct {
	ARN          string
	ID           string
	Name         string
	Version      string
	Status       string
	Description  string
	CreationTime time.Time
	Tags         map[string]string
}

// Agent is the scanner-owned representation of one Bedrock agent. It carries
// the name, description, and foundation model id only. The agent instruction
// (system prompt) and prompt-override configuration are valuable IP and are
// never read or persisted: there is no field for either.
type Agent struct {
	ARN             string
	ID              string
	Name            string
	Status          string
	Description     string
	FoundationModel string
	// KnowledgeBaseIDs lists the knowledge bases associated with the agent.
	KnowledgeBaseIDs []string
	CreationTime     time.Time
	Tags             map[string]string
}

// AgentActionGroup is the scanner-owned representation of one Bedrock agent
// action group. It carries the name and the Lambda executor ARN only. The
// action-group API schema body and function schema are often customer IP and
// are never read or persisted: there is no field for either.
type AgentActionGroup struct {
	AgentID   string
	ID        string
	Name      string
	State     string
	LambdaARN string
}

// KnowledgeBase is the scanner-owned representation of one Bedrock knowledge
// base. It carries the name, status, and embedding model reference plus the
// reported data source endpoints (S3 bucket ARN, Confluence/SharePoint/web
// URLs). Ingested document content and chunks are never read or persisted:
// there is no field for them.
type KnowledgeBase struct {
	ARN               string
	ID                string
	Name              string
	Status            string
	Description       string
	EmbeddingModelARN string
	DataSources       []KnowledgeBaseDataSource
	CreationTime      time.Time
	Tags              map[string]string
}

// KnowledgeBaseDataSource is the scanner-owned representation of one knowledge
// base data source endpoint reference. It carries only the connector type and
// the ARN or URL that addresses the source, never any ingested content.
type KnowledgeBaseDataSource struct {
	ID   string
	Name string
	Type string
	// S3BucketARN is set for an S3 connector.
	S3BucketARN string
	// URL is set for Confluence (host URL), SharePoint (site URL), or
	// web-crawler (seed URL) connectors.
	URL string
}
