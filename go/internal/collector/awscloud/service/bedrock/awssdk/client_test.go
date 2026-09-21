// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awsbedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	awsbedrockagent "github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	awsbedrockagenttypes "github.com/aws/aws-sdk-go-v2/service/bedrockagent/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListCustomModelsReadsJobAndOutputNotHyperParameters(t *testing.T) {
	bedrockAPI := &fakeBedrockAPI{
		customModels: []awsbedrocktypes.CustomModelSummary{{
			ModelArn:     aws.String("arn:aws:bedrock:us-east-1:123456789012:custom-model/cm"),
			ModelName:    aws.String("cm"),
			BaseModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3"),
		}},
		getCustomModel: &awsbedrock.GetCustomModelOutput{
			ModelArn:         aws.String("arn:aws:bedrock:us-east-1:123456789012:custom-model/cm"),
			ModelName:        aws.String("cm"),
			BaseModelArn:     aws.String("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-3"),
			JobArn:           aws.String("arn:aws:bedrock:us-east-1:123456789012:model-customization-job/job-1"),
			OutputDataConfig: &awsbedrocktypes.OutputDataConfig{S3Uri: aws.String("s3://custom-model-output/cm/")},
			// HyperParameters and TrainingDataConfig are populated to prove the
			// adapter ignores them; the scanner-owned type has no field for either.
			HyperParameters:    map[string]string{"learning_rate": "secret-hyperparameter"},
			TrainingDataConfig: &awsbedrocktypes.TrainingDataConfig{S3Uri: aws.String("s3://training/custom-model/input")},
		},
	}
	adapter := newTestClient(bedrockAPI, &fakeBedrockAgentAPI{})

	models, err := adapter.ListCustomModels(context.Background())
	if err != nil {
		t.Fatalf("ListCustomModels() error = %v, want nil", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	model := models[0]
	if model.JobARN != "arn:aws:bedrock:us-east-1:123456789012:model-customization-job/job-1" {
		t.Fatalf("JobARN = %q", model.JobARN)
	}
	if model.OutputS3URI != "s3://custom-model-output/cm/" {
		t.Fatalf("OutputS3URI = %q", model.OutputS3URI)
	}
	// The scanner-owned CustomModel has no hyperparameter or training-input field,
	// so the secret value and training input URI cannot be carried forward.
}

func TestClientListGuardrailsNeverCallsGetGuardrail(t *testing.T) {
	bedrockAPI := &fakeBedrockAPI{
		guardrails: []awsbedrocktypes.GuardrailSummary{{
			Arn:     aws.String("arn:aws:bedrock:us-east-1:123456789012:guardrail/gr"),
			Id:      aws.String("gr-1"),
			Name:    aws.String("content-guardrail"),
			Version: aws.String("DRAFT"),
			Status:  awsbedrocktypes.GuardrailStatusReady,
		}},
	}
	adapter := newTestClient(bedrockAPI, &fakeBedrockAgentAPI{})

	guardrails, err := adapter.ListGuardrails(context.Background())
	if err != nil {
		t.Fatalf("ListGuardrails() error = %v, want nil", err)
	}
	if len(guardrails) != 1 || guardrails[0].Name != "content-guardrail" {
		t.Fatalf("guardrails = %#v", guardrails)
	}
	// GetGuardrail is the only operation returning topic/content policy bodies.
	// The adapter must never call it, and it is not even on the apiClient
	// interface, so a policy body has no path into scanner state.
	if bedrockAPI.calledOps["GetGuardrail"] {
		t.Fatalf("adapter called GetGuardrail; guardrail policy bodies must stay unread")
	}
}

func TestClientListAgentsReadsFoundationModelNotInstruction(t *testing.T) {
	agentAPI := &fakeBedrockAgentAPI{
		agents: []awsbedrockagenttypes.AgentSummary{{
			AgentId:     aws.String("AG1"),
			AgentName:   aws.String("order-agent"),
			AgentStatus: awsbedrockagenttypes.AgentStatusPrepared,
			Description: aws.String("handles orders"),
		}},
		getAgent: &awsbedrockagent.GetAgentOutput{
			Agent: &awsbedrockagenttypes.Agent{
				AgentArn:        aws.String("arn:aws:bedrock:us-east-1:123456789012:agent/AG1"),
				AgentId:         aws.String("AG1"),
				AgentName:       aws.String("order-agent"),
				FoundationModel: aws.String("anthropic.claude-3"),
				// Instruction and PromptOverrideConfiguration are populated to prove
				// the adapter ignores them; the scanner-owned Agent type has no field.
				Instruction: aws.String("you-are-a-helpful-secret-agent-prompt"),
				PromptOverrideConfiguration: &awsbedrockagenttypes.PromptOverrideConfiguration{
					PromptConfigurations: []awsbedrockagenttypes.PromptConfiguration{{
						BasePromptTemplate: aws.String("prompt-override-template-body"),
					}},
				},
			},
		},
		agentKnowledgeRefs: []awsbedrockagenttypes.AgentKnowledgeBaseSummary{{
			KnowledgeBaseId: aws.String("KB1"),
		}},
	}
	adapter := newTestClient(&fakeBedrockAPI{}, agentAPI)

	agents, err := adapter.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v, want nil", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}
	agent := agents[0]
	if agent.FoundationModel != "anthropic.claude-3" {
		t.Fatalf("FoundationModel = %q", agent.FoundationModel)
	}
	if len(agent.KnowledgeBaseIDs) != 1 || agent.KnowledgeBaseIDs[0] != "KB1" {
		t.Fatalf("KnowledgeBaseIDs = %#v, want [KB1]", agent.KnowledgeBaseIDs)
	}
	// The scanner-owned Agent type has no Instruction or PromptOverride field, so
	// neither sentinel value can be carried forward.
}

func TestClientListAgentActionGroupsReadsLambdaNotApiSchema(t *testing.T) {
	agentAPI := &fakeBedrockAgentAPI{
		agents: []awsbedrockagenttypes.AgentSummary{{
			AgentId:     aws.String("AG1"),
			AgentName:   aws.String("order-agent"),
			AgentStatus: awsbedrockagenttypes.AgentStatusPrepared,
		}},
		actionGroups: []awsbedrockagenttypes.ActionGroupSummary{{
			ActionGroupId:    aws.String("ACT1"),
			ActionGroupName:  aws.String("order-tools"),
			ActionGroupState: awsbedrockagenttypes.ActionGroupStateEnabled,
		}},
		getActionGroup: &awsbedrockagent.GetAgentActionGroupOutput{
			AgentActionGroup: &awsbedrockagenttypes.AgentActionGroup{
				ActionGroupId:   aws.String("ACT1"),
				ActionGroupName: aws.String("order-tools"),
				ActionGroupExecutor: &awsbedrockagenttypes.ActionGroupExecutorMemberLambda{
					Value: "arn:aws:lambda:us-east-1:123456789012:function:order-tool",
				},
				// ApiSchema is populated to prove the adapter ignores it.
				ApiSchema: &awsbedrockagenttypes.APISchemaMemberPayload{Value: "customer-ip-action-api-schema"},
			},
		},
	}
	adapter := newTestClient(&fakeBedrockAPI{}, agentAPI)

	groups, err := adapter.ListAgentActionGroups(context.Background())
	if err != nil {
		t.Fatalf("ListAgentActionGroups() error = %v, want nil", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	group := groups[0]
	if group.LambdaARN != "arn:aws:lambda:us-east-1:123456789012:function:order-tool" {
		t.Fatalf("LambdaARN = %q", group.LambdaARN)
	}
	if group.AgentID != "AG1" {
		t.Fatalf("AgentID = %q, want AG1", group.AgentID)
	}
	// The scanner-owned AgentActionGroup has no schema field, so the IP schema
	// payload cannot be carried forward.
}

func TestClientListKnowledgeBasesReadsEmbeddingAndDataSourcesNotContent(t *testing.T) {
	agentAPI := &fakeBedrockAgentAPI{
		knowledgeBases: []awsbedrockagenttypes.KnowledgeBaseSummary{{
			KnowledgeBaseId: aws.String("KB1"),
			Name:            aws.String("docs-kb"),
			Status:          awsbedrockagenttypes.KnowledgeBaseStatusActive,
		}},
		getKnowledgeBase: &awsbedrockagent.GetKnowledgeBaseOutput{
			KnowledgeBase: &awsbedrockagenttypes.KnowledgeBase{
				KnowledgeBaseArn: aws.String("arn:aws:bedrock:us-east-1:123456789012:knowledge-base/KB1"),
				KnowledgeBaseId:  aws.String("KB1"),
				Name:             aws.String("docs-kb"),
				KnowledgeBaseConfiguration: &awsbedrockagenttypes.KnowledgeBaseConfiguration{
					Type: awsbedrockagenttypes.KnowledgeBaseTypeVector,
					VectorKnowledgeBaseConfiguration: &awsbedrockagenttypes.VectorKnowledgeBaseConfiguration{
						EmbeddingModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/amazon.titan-embed"),
					},
				},
			},
		},
		dataSources: []awsbedrockagenttypes.DataSourceSummary{{
			DataSourceId:    aws.String("DS-S3"),
			KnowledgeBaseId: aws.String("KB1"),
			Name:            aws.String("s3-docs"),
		}},
		getDataSource: &awsbedrockagent.GetDataSourceOutput{
			DataSource: &awsbedrockagenttypes.DataSource{
				DataSourceId:    aws.String("DS-S3"),
				KnowledgeBaseId: aws.String("KB1"),
				Name:            aws.String("s3-docs"),
				DataSourceConfiguration: &awsbedrockagenttypes.DataSourceConfiguration{
					Type:            awsbedrockagenttypes.DataSourceTypeS3,
					S3Configuration: &awsbedrockagenttypes.S3DataSourceConfiguration{BucketArn: aws.String("arn:aws:s3:::kb-docs")},
				},
			},
		},
	}
	adapter := newTestClient(&fakeBedrockAPI{}, agentAPI)

	bases, err := adapter.ListKnowledgeBases(context.Background())
	if err != nil {
		t.Fatalf("ListKnowledgeBases() error = %v, want nil", err)
	}
	if len(bases) != 1 {
		t.Fatalf("len(bases) = %d, want 1", len(bases))
	}
	kb := bases[0]
	if kb.EmbeddingModelARN != "arn:aws:bedrock:us-east-1::foundation-model/amazon.titan-embed" {
		t.Fatalf("EmbeddingModelARN = %q", kb.EmbeddingModelARN)
	}
	if len(kb.DataSources) != 1 {
		t.Fatalf("len(DataSources) = %d, want 1", len(kb.DataSources))
	}
	if kb.DataSources[0].S3BucketARN != "arn:aws:s3:::kb-docs" {
		t.Fatalf("data source S3 bucket ARN = %q", kb.DataSources[0].S3BucketARN)
	}
	if strings.ToUpper(kb.DataSources[0].Type) != "S3" {
		t.Fatalf("data source type = %q, want S3", kb.DataSources[0].Type)
	}
	// GetKnowledgeBaseDocuments / ListKnowledgeBaseDocuments are the only
	// operations returning ingested document content. They are not on the adapter
	// interface, so document content has no path into scanner state.
	if agentAPI.calledOps["GetKnowledgeBaseDocuments"] || agentAPI.calledOps["ListKnowledgeBaseDocuments"] {
		t.Fatalf("adapter read knowledge base documents; ingested content must stay unread")
	}
}

func TestClientWebDataSourceResolvesSeedURL(t *testing.T) {
	agentAPI := &fakeBedrockAgentAPI{
		knowledgeBases: []awsbedrockagenttypes.KnowledgeBaseSummary{{
			KnowledgeBaseId: aws.String("KB1"),
			Name:            aws.String("docs-kb"),
		}},
		getKnowledgeBase: &awsbedrockagent.GetKnowledgeBaseOutput{
			KnowledgeBase: &awsbedrockagenttypes.KnowledgeBase{
				KnowledgeBaseArn: aws.String("arn:aws:bedrock:us-east-1:123456789012:knowledge-base/KB1"),
				KnowledgeBaseId:  aws.String("KB1"),
			},
		},
		dataSources: []awsbedrockagenttypes.DataSourceSummary{{
			DataSourceId:    aws.String("DS-WEB"),
			KnowledgeBaseId: aws.String("KB1"),
			Name:            aws.String("web"),
		}},
		getDataSource: &awsbedrockagent.GetDataSourceOutput{
			DataSource: &awsbedrockagenttypes.DataSource{
				DataSourceId: aws.String("DS-WEB"),
				DataSourceConfiguration: &awsbedrockagenttypes.DataSourceConfiguration{
					Type: awsbedrockagenttypes.DataSourceTypeWeb,
					WebConfiguration: &awsbedrockagenttypes.WebDataSourceConfiguration{
						SourceConfiguration: &awsbedrockagenttypes.WebSourceConfiguration{
							UrlConfiguration: &awsbedrockagenttypes.UrlConfiguration{
								SeedUrls: []awsbedrockagenttypes.SeedUrl{{Url: aws.String("https://docs.example.com")}},
							},
						},
					},
				},
			},
		},
	}
	adapter := newTestClient(&fakeBedrockAPI{}, agentAPI)

	bases, err := adapter.ListKnowledgeBases(context.Background())
	if err != nil {
		t.Fatalf("ListKnowledgeBases() error = %v, want nil", err)
	}
	if len(bases) != 1 || len(bases[0].DataSources) != 1 {
		t.Fatalf("bases = %#v", bases)
	}
	if bases[0].DataSources[0].URL != "https://docs.example.com" {
		t.Fatalf("web data source URL = %q", bases[0].DataSources[0].URL)
	}
}

func newTestClient(bedrockAPI bedrockAPIClient, agentAPI bedrockAgentAPIClient) *Client {
	return &Client{
		bedrock:  bedrockAPI,
		agent:    agentAPI,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBedrock},
	}
}
