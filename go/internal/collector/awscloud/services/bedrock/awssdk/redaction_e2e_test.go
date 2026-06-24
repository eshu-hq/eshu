// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awsbedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	awsbedrockagent "github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	awsbedrockagenttypes "github.com/aws/aws-sdk-go-v2/service/bedrockagent/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	bedrocksvc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/bedrock"
)

// bannedSentinels are the high-IP payloads the Bedrock scanner must never
// persist. Each one is injected into a *consumed SDK output field* in the
// fixture below so the proof exercises the real redaction boundary: the
// awssdk.Client adapter reads the populated SDK responses and must drop these
// values when mapping into scanner-owned types, after which the bedrock.Scanner
// emits facts. None of the sentinels may appear in any emitted fact.
var bannedSentinels = []string{
	"you-are-a-helpful-secret-agent-prompt", // GetAgentOutput.Agent.Instruction (system prompt)
	"prompt-override-template-body",         // GetAgentOutput.Agent.PromptOverrideConfiguration body
	"customer-ip-action-api-schema",         // GetAgentActionGroupOutput.ApiSchema body
	"function-schema-secret-body",           // GetAgentActionGroupOutput.FunctionSchema body
	"secret-hyperparameter",                 // GetCustomModelOutput.HyperParameters value
	"s3://training/custom-model/input",      // GetCustomModelOutput.TrainingDataConfig.S3Uri
}

// TestScannerNeverEmitsSDKSentinelsEndToEnd is the load-bearing redaction proof
// for this high-IP scanner. Earlier scanner-package tests scanned facts built
// from the scanner-owned fixture types, which have no field for these payloads,
// so they were vacuous: the sentinels were never present in the inputs. This
// test injects every reachable sentinel into the *SDK output layer the adapter
// actually reads from*, drives the real awssdk.Client adapter through a full
// bedrock.Scanner run, and proves none of the sentinels survives into any
// emitted fact. The adapter is the redaction boundary; the scanner-owned types
// are downstream of it.
//
// Non-vacuity is enforced two ways. First, the fixture asserts each sentinel is
// actually present in the SDK responses (sentinelsArePresentInSDK), so a future
// edit that drops a sentinel from the fixture fails loudly instead of making the
// test pass for the wrong reason. Second, this test was confirmed to FAIL when
// the adapter was temporarily edited to copy GetAgentOutput.Agent.Instruction
// into a scanner field (see the PR thread); reverting restored green. Two
// sentinels (guardrail topic/content policy bodies and knowledge base ingested
// document content) are intentionally absent from this list because they are
// not reachable through the consumed SDK interface at all: GetGuardrail and
// Get/ListKnowledgeBaseDocuments are not adapter methods. exclusion_test.go and
// client_test.go prove those read paths cannot exist.
func TestScannerNeverEmitsSDKSentinelsEndToEnd(t *testing.T) {
	bedrockAPI, agentAPI := sentinelLadenSDK()

	sentinelsArePresentInSDK(t, bedrockAPI, agentAPI)

	adapter := newTestClient(bedrockAPI, agentAPI)
	scanner := bedrocksvc.Scanner{Client: adapter}
	envelopes, err := scanner.Scan(context.Background(), redactionBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() emitted no envelopes; the fixture must exercise the full pipeline")
	}

	for _, envelope := range envelopes {
		serialized, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope %q: %v", envelope.FactKind, err)
		}
		flat := strings.ToLower(string(serialized))
		for _, banned := range bannedSentinels {
			if strings.Contains(flat, strings.ToLower(banned)) {
				t.Fatalf("emitted fact %q contains forbidden SDK payload %q: %s", envelope.FactKind, banned, flat)
			}
		}
	}
}

// sentinelsArePresentInSDK fails the test if any banned sentinel is missing from
// the raw SDK fixture. It guards the redaction proof against silently regressing
// into a vacuous test: if the inputs no longer carry a sentinel, the absence in
// the emitted facts proves nothing.
func sentinelsArePresentInSDK(t *testing.T, bedrockAPI *fakeBedrockAPI, agentAPI *fakeBedrockAgentAPI) {
	t.Helper()
	rawAgent, err := json.Marshal(agentAPI.getAgent)
	if err != nil {
		t.Fatalf("marshal getAgent: %v", err)
	}
	rawActionGroup, err := json.Marshal(agentAPI.getActionGroup)
	if err != nil {
		t.Fatalf("marshal getActionGroup: %v", err)
	}
	rawModel, err := json.Marshal(bedrockAPI.getCustomModel)
	if err != nil {
		t.Fatalf("marshal getCustomModel: %v", err)
	}
	// The APISchema/FunctionSchema/ActionGroupExecutor union members carry
	// unexported fields the JSON marshaler skips, so assert those sentinels via
	// the typed fixture rather than the serialized bytes.
	rawSDK := strings.ToLower(string(rawAgent) + string(rawActionGroup) + string(rawModel))
	typedSentinels := actionGroupSchemaSentinels(agentAPI.getActionGroup)
	for _, banned := range bannedSentinels {
		lowered := strings.ToLower(banned)
		if strings.Contains(rawSDK, lowered) {
			continue
		}
		if typedSentinels[lowered] {
			continue
		}
		t.Fatalf("sentinel %q is absent from the SDK fixture; the redaction proof would be vacuous", banned)
	}
}

// actionGroupSchemaSentinels extracts the API/function schema payload strings
// from a GetAgentActionGroup fixture. The SDK union members hold their payload
// behind unexported fields, so JSON marshaling does not surface them; this
// reads the typed values directly to confirm the sentinels are seeded.
func actionGroupSchemaSentinels(output *awsbedrockagent.GetAgentActionGroupOutput) map[string]bool {
	present := map[string]bool{}
	if output == nil || output.AgentActionGroup == nil {
		return present
	}
	if api, ok := output.AgentActionGroup.ApiSchema.(*awsbedrockagenttypes.APISchemaMemberPayload); ok {
		present[strings.ToLower(api.Value)] = true
	}
	if fn, ok := output.AgentActionGroup.FunctionSchema.(*awsbedrockagenttypes.FunctionSchemaMemberFunctions); ok {
		for _, function := range fn.Value {
			present[strings.ToLower(aws.ToString(function.Description))] = true
		}
	}
	return present
}

// sentinelLadenSDK returns SDK fixtures whose consumed read paths carry every
// reachable banned sentinel, so the full scan exercises the adapter redaction
// boundary against populated IP rather than absent inputs.
func sentinelLadenSDK() (*fakeBedrockAPI, *fakeBedrockAgentAPI) {
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
			// IP fields the adapter must never copy.
			HyperParameters:    map[string]string{"learning_rate": "secret-hyperparameter"},
			TrainingDataConfig: &awsbedrocktypes.TrainingDataConfig{S3Uri: aws.String("s3://training/custom-model/input")},
		},
		guardrails: []awsbedrocktypes.GuardrailSummary{{
			Arn:     aws.String("arn:aws:bedrock:us-east-1:123456789012:guardrail/gr"),
			Id:      aws.String("gr-1"),
			Name:    aws.String("content-guardrail"),
			Version: aws.String("DRAFT"),
			Status:  awsbedrocktypes.GuardrailStatusReady,
			// Description is metadata the scanner persists; it must stay
			// sentinel-free so this fixture does not falsely trip the scan.
			Description: aws.String("blocks unsafe content"),
		}},
	}

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
				// IP fields the adapter must never copy.
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
				// IP fields the adapter must never copy.
				ApiSchema: &awsbedrockagenttypes.APISchemaMemberPayload{Value: "customer-ip-action-api-schema"},
				FunctionSchema: &awsbedrockagenttypes.FunctionSchemaMemberFunctions{
					Value: []awsbedrockagenttypes.Function{{
						Name:        aws.String("placeOrder"),
						Description: aws.String("function-schema-secret-body"),
					}},
				},
			},
		},
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
	return bedrockAPI, agentAPI
}

// redactionBoundary is the claimed boundary the redaction scan runs under. It
// carries the fields envelope validation requires so the scan emits real facts.
func redactionBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceBedrock,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:bedrock:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}
