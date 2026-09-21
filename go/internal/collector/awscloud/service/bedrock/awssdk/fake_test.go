// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"
	awsbedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	awsbedrockagent "github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	awsbedrockagenttypes "github.com/aws/aws-sdk-go-v2/service/bedrockagent/types"
)

// fakeBedrockAPI is a single-page bedrockAPIClient double. Each List returns its
// seeded slice once; each Get returns its seeded output. calledOps records every
// operation invoked so tests can assert a forbidden read was never reached.
type fakeBedrockAPI struct {
	foundationModels  []awsbedrocktypes.FoundationModelSummary
	customModels      []awsbedrocktypes.CustomModelSummary
	getCustomModel    *awsbedrock.GetCustomModelOutput
	customizationJobs []awsbedrocktypes.ModelCustomizationJobSummary
	provisionedModels []awsbedrocktypes.ProvisionedModelSummary
	guardrails        []awsbedrocktypes.GuardrailSummary
	tags              []awsbedrocktypes.Tag

	calledOps map[string]bool
}

func (f *fakeBedrockAPI) record(op string) {
	if f.calledOps == nil {
		f.calledOps = map[string]bool{}
	}
	f.calledOps[op] = true
}

func (f *fakeBedrockAPI) ListFoundationModels(_ context.Context, _ *awsbedrock.ListFoundationModelsInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListFoundationModelsOutput, error) {
	f.record("ListFoundationModels")
	return &awsbedrock.ListFoundationModelsOutput{ModelSummaries: f.foundationModels}, nil
}

func (f *fakeBedrockAPI) ListCustomModels(_ context.Context, _ *awsbedrock.ListCustomModelsInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListCustomModelsOutput, error) {
	f.record("ListCustomModels")
	return &awsbedrock.ListCustomModelsOutput{ModelSummaries: f.customModels}, nil
}

func (f *fakeBedrockAPI) GetCustomModel(_ context.Context, _ *awsbedrock.GetCustomModelInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.GetCustomModelOutput, error) {
	f.record("GetCustomModel")
	return f.getCustomModel, nil
}

func (f *fakeBedrockAPI) ListModelCustomizationJobs(_ context.Context, _ *awsbedrock.ListModelCustomizationJobsInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListModelCustomizationJobsOutput, error) {
	f.record("ListModelCustomizationJobs")
	return &awsbedrock.ListModelCustomizationJobsOutput{ModelCustomizationJobSummaries: f.customizationJobs}, nil
}

func (f *fakeBedrockAPI) ListProvisionedModelThroughputs(_ context.Context, _ *awsbedrock.ListProvisionedModelThroughputsInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListProvisionedModelThroughputsOutput, error) {
	f.record("ListProvisionedModelThroughputs")
	return &awsbedrock.ListProvisionedModelThroughputsOutput{ProvisionedModelSummaries: f.provisionedModels}, nil
}

func (f *fakeBedrockAPI) ListGuardrails(_ context.Context, _ *awsbedrock.ListGuardrailsInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListGuardrailsOutput, error) {
	f.record("ListGuardrails")
	return &awsbedrock.ListGuardrailsOutput{Guardrails: f.guardrails}, nil
}

func (f *fakeBedrockAPI) ListTagsForResource(_ context.Context, _ *awsbedrock.ListTagsForResourceInput, _ ...func(*awsbedrock.Options)) (*awsbedrock.ListTagsForResourceOutput, error) {
	f.record("ListTagsForResource")
	return &awsbedrock.ListTagsForResourceOutput{Tags: f.tags}, nil
}

var _ bedrockAPIClient = (*fakeBedrockAPI)(nil)

// fakeBedrockAgentAPI is a single-page bedrockAgentAPIClient double.
type fakeBedrockAgentAPI struct {
	agents             []awsbedrockagenttypes.AgentSummary
	getAgent           *awsbedrockagent.GetAgentOutput
	agentKnowledgeRefs []awsbedrockagenttypes.AgentKnowledgeBaseSummary
	actionGroups       []awsbedrockagenttypes.ActionGroupSummary
	getActionGroup     *awsbedrockagent.GetAgentActionGroupOutput
	knowledgeBases     []awsbedrockagenttypes.KnowledgeBaseSummary
	getKnowledgeBase   *awsbedrockagent.GetKnowledgeBaseOutput
	dataSources        []awsbedrockagenttypes.DataSourceSummary
	getDataSource      *awsbedrockagent.GetDataSourceOutput
	tags               map[string]string

	calledOps map[string]bool
}

func (f *fakeBedrockAgentAPI) record(op string) {
	if f.calledOps == nil {
		f.calledOps = map[string]bool{}
	}
	f.calledOps[op] = true
}

func (f *fakeBedrockAgentAPI) ListAgents(_ context.Context, _ *awsbedrockagent.ListAgentsInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentsOutput, error) {
	f.record("ListAgents")
	return &awsbedrockagent.ListAgentsOutput{AgentSummaries: f.agents}, nil
}

func (f *fakeBedrockAgentAPI) GetAgent(_ context.Context, _ *awsbedrockagent.GetAgentInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetAgentOutput, error) {
	f.record("GetAgent")
	return f.getAgent, nil
}

func (f *fakeBedrockAgentAPI) ListAgentKnowledgeBases(_ context.Context, _ *awsbedrockagent.ListAgentKnowledgeBasesInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentKnowledgeBasesOutput, error) {
	f.record("ListAgentKnowledgeBases")
	return &awsbedrockagent.ListAgentKnowledgeBasesOutput{AgentKnowledgeBaseSummaries: f.agentKnowledgeRefs}, nil
}

func (f *fakeBedrockAgentAPI) ListAgentActionGroups(_ context.Context, _ *awsbedrockagent.ListAgentActionGroupsInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListAgentActionGroupsOutput, error) {
	f.record("ListAgentActionGroups")
	return &awsbedrockagent.ListAgentActionGroupsOutput{ActionGroupSummaries: f.actionGroups}, nil
}

func (f *fakeBedrockAgentAPI) GetAgentActionGroup(_ context.Context, _ *awsbedrockagent.GetAgentActionGroupInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetAgentActionGroupOutput, error) {
	f.record("GetAgentActionGroup")
	return f.getActionGroup, nil
}

func (f *fakeBedrockAgentAPI) ListKnowledgeBases(_ context.Context, _ *awsbedrockagent.ListKnowledgeBasesInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListKnowledgeBasesOutput, error) {
	f.record("ListKnowledgeBases")
	return &awsbedrockagent.ListKnowledgeBasesOutput{KnowledgeBaseSummaries: f.knowledgeBases}, nil
}

func (f *fakeBedrockAgentAPI) GetKnowledgeBase(_ context.Context, _ *awsbedrockagent.GetKnowledgeBaseInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetKnowledgeBaseOutput, error) {
	f.record("GetKnowledgeBase")
	return f.getKnowledgeBase, nil
}

func (f *fakeBedrockAgentAPI) ListDataSources(_ context.Context, _ *awsbedrockagent.ListDataSourcesInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListDataSourcesOutput, error) {
	f.record("ListDataSources")
	return &awsbedrockagent.ListDataSourcesOutput{DataSourceSummaries: f.dataSources}, nil
}

func (f *fakeBedrockAgentAPI) GetDataSource(_ context.Context, _ *awsbedrockagent.GetDataSourceInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.GetDataSourceOutput, error) {
	f.record("GetDataSource")
	return f.getDataSource, nil
}

func (f *fakeBedrockAgentAPI) ListTagsForResource(_ context.Context, _ *awsbedrockagent.ListTagsForResourceInput, _ ...func(*awsbedrockagent.Options)) (*awsbedrockagent.ListTagsForResourceOutput, error) {
	f.record("ListTagsForResource")
	return &awsbedrockagent.ListTagsForResourceOutput{Tags: f.tags}, nil
}

var _ bedrockAgentAPIClient = (*fakeBedrockAgentAPI)(nil)
