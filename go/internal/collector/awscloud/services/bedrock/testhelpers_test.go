// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"context"
)

// fakeClient is a static Bedrock Client double. Each List returns its seeded
// slice (or seeded error) so scanner tests exercise resource and relationship
// emission without any AWS SDK dependency.
type fakeClient struct {
	foundationModels  []FoundationModel
	customModels      []CustomModel
	customizationJobs []ModelCustomizationJob
	provisionedModels []ProvisionedModelThroughput
	guardrails        []Guardrail
	agents            []Agent
	actionGroups      []AgentActionGroup
	knowledgeBases    []KnowledgeBase
	guardrailErr      error
}

func (f *fakeClient) ListFoundationModels(context.Context) ([]FoundationModel, error) {
	return f.foundationModels, nil
}

func (f *fakeClient) ListCustomModels(context.Context) ([]CustomModel, error) {
	return f.customModels, nil
}

func (f *fakeClient) ListModelCustomizationJobs(context.Context) ([]ModelCustomizationJob, error) {
	return f.customizationJobs, nil
}

func (f *fakeClient) ListProvisionedModelThroughputs(context.Context) ([]ProvisionedModelThroughput, error) {
	return f.provisionedModels, nil
}

func (f *fakeClient) ListGuardrails(context.Context) ([]Guardrail, error) {
	return f.guardrails, f.guardrailErr
}

func (f *fakeClient) ListAgents(context.Context) ([]Agent, error) {
	return f.agents, nil
}

func (f *fakeClient) ListAgentActionGroups(context.Context) ([]AgentActionGroup, error) {
	return f.actionGroups, nil
}

func (f *fakeClient) ListKnowledgeBases(context.Context) ([]KnowledgeBase, error) {
	return f.knowledgeBases, nil
}

var _ Client = (*fakeClient)(nil)

// richClient returns a fully populated fixture covering every in-scope resource
// type and relationship. Sensitive sentinel values are deliberately absent: the
// fixture proves the scanner cannot emit forbidden payloads because the
// scanner-owned types have no field to carry them.
func richClient() *fakeClient {
	return &fakeClient{
		foundationModels: []FoundationModel{{
			ARN:             "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3",
			ModelID:         "anthropic.claude-3",
			ProviderName:    "Anthropic",
			LifecycleStatus: "ACTIVE",
		}},
		customModels: []CustomModel{{
			ARN:          "arn:aws:bedrock:us-east-1:123456789012:custom-model/cm",
			Name:         "cm",
			BaseModelARN: "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3",
			JobARN:       "arn:aws:bedrock:us-east-1:123456789012:model-customization-job/job-1",
			OutputS3URI:  "s3://custom-model-output/cm/",
			Tags:         map[string]string{"Team": "ml"},
		}},
		customizationJobs: []ModelCustomizationJob{{
			ARN:            "arn:aws:bedrock:us-east-1:123456789012:model-customization-job/job-1",
			Name:           "job-1",
			Status:         "Completed",
			BaseModelARN:   "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3",
			CustomModelARN: "arn:aws:bedrock:us-east-1:123456789012:custom-model/cm",
		}},
		provisionedModels: []ProvisionedModelThroughput{{
			ARN:        "arn:aws:bedrock:us-east-1:123456789012:provisioned-model/pm",
			Name:       "pm",
			Status:     "InService",
			ModelARN:   "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3",
			ModelUnits: 2,
		}},
		guardrails: []Guardrail{{
			ARN:     "arn:aws:bedrock:us-east-1:123456789012:guardrail/gr",
			ID:      "gr-1",
			Name:    "content-guardrail",
			Version: "DRAFT",
			Status:  "READY",
		}},
		agents: []Agent{{
			ARN:              "arn:aws:bedrock:us-east-1:123456789012:agent/AG1",
			ID:               "AG1",
			Name:             "order-agent",
			Status:           "PREPARED",
			Description:      "handles orders",
			FoundationModel:  "anthropic.claude-3",
			KnowledgeBaseIDs: []string{"KB1"},
		}},
		actionGroups: []AgentActionGroup{{
			AgentID:   "AG1",
			ID:        "ACT1",
			Name:      "order-tools",
			State:     "ENABLED",
			LambdaARN: "arn:aws:lambda:us-east-1:123456789012:function:order-tool",
		}},
		knowledgeBases: []KnowledgeBase{{
			ARN:               "arn:aws:bedrock:us-east-1:123456789012:knowledge-base/KB1",
			ID:                "KB1",
			Name:              "docs-kb",
			Status:            "ACTIVE",
			EmbeddingModelARN: "arn:aws:bedrock:us-east-1::foundation-model/amazon.titan-embed",
			DataSources: []KnowledgeBaseDataSource{
				{ID: "DS-S3", Name: "s3-docs", Type: "S3", S3BucketARN: "arn:aws:s3:::kb-docs"},
				{ID: "DS-CONF", Name: "wiki", Type: "CONFLUENCE", URL: "https://wiki.example.com"},
				{ID: "DS-SP", Name: "sp", Type: "SHAREPOINT", URL: "https://sharepoint.example.com/sites/kb"},
				{ID: "DS-WEB", Name: "web", Type: "WEB", URL: "https://docs.example.com"},
			},
		}},
	}
}
