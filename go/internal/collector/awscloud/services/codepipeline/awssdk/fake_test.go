// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodepipeline "github.com/aws/aws-sdk-go-v2/service/codepipeline"
	cptypes "github.com/aws/aws-sdk-go-v2/service/codepipeline/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codepipeline"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func newTestClient(api apiClient, key redact.Key) *Client {
	return &Client{
		client:       api,
		boundary:     testBoundary(),
		redactionKey: key,
	}
}

// fakeCodePipelineAPI is a metadata-only CodePipeline SDK stub. It only
// implements the read operations the adapter consumes; the apiClient interface
// guard test ensures no mutation, execution-control, webhook-management, or
// job-worker method is reachable.
type fakeCodePipelineAPI struct {
	// pipelineNames yields a single ListPipelines page.
	pipelineNames []string
	// pipelinePages yields one ListPipelines page per slice element, exercising
	// nextToken continuation.
	pipelinePages [][]string
	pipelines     map[string]cptypes.PipelineDeclaration
	// metadata yields the GetPipeline PipelineMetadata for a pipeline name,
	// carrying the canonical ARN plus the Created/Updated timestamps.
	metadata             map[string]*cptypes.PipelineMetadata
	executionsByPipeline map[string][]cptypes.PipelineExecutionSummary
	webhooks             []cptypes.ListWebhookItem
	actionTypes          []cptypes.ActionType
	tags                 map[string]map[string]string

	listPipelinesCalls int
}

func (f *fakeCodePipelineAPI) ListPipelines(
	_ context.Context,
	input *awscodepipeline.ListPipelinesInput,
	_ ...func(*awscodepipeline.Options),
) (*awscodepipeline.ListPipelinesOutput, error) {
	f.listPipelinesCalls++
	if len(f.pipelinePages) > 0 {
		index := 0
		if input.NextToken != nil {
			index = int((*input.NextToken)[0] - '0')
		}
		if index >= len(f.pipelinePages) {
			return &awscodepipeline.ListPipelinesOutput{}, nil
		}
		summaries := make([]cptypes.PipelineSummary, 0, len(f.pipelinePages[index]))
		for _, name := range f.pipelinePages[index] {
			summaries = append(summaries, cptypes.PipelineSummary{Name: aws.String(name)})
		}
		var next *string
		if index+1 < len(f.pipelinePages) {
			token := string(rune('0' + index + 1))
			next = aws.String(token)
		}
		return &awscodepipeline.ListPipelinesOutput{Pipelines: summaries, NextToken: next}, nil
	}
	summaries := make([]cptypes.PipelineSummary, 0, len(f.pipelineNames))
	for _, name := range f.pipelineNames {
		summaries = append(summaries, cptypes.PipelineSummary{Name: aws.String(name)})
	}
	return &awscodepipeline.ListPipelinesOutput{Pipelines: summaries}, nil
}

func (f *fakeCodePipelineAPI) GetPipeline(
	_ context.Context,
	input *awscodepipeline.GetPipelineInput,
	_ ...func(*awscodepipeline.Options),
) (*awscodepipeline.GetPipelineOutput, error) {
	name := aws.ToString(input.Name)
	decl, ok := f.pipelines[name]
	if !ok {
		return &awscodepipeline.GetPipelineOutput{}, nil
	}
	return &awscodepipeline.GetPipelineOutput{Pipeline: &decl, Metadata: f.metadata[name]}, nil
}

func (f *fakeCodePipelineAPI) ListPipelineExecutions(
	_ context.Context,
	input *awscodepipeline.ListPipelineExecutionsInput,
	_ ...func(*awscodepipeline.Options),
) (*awscodepipeline.ListPipelineExecutionsOutput, error) {
	name := aws.ToString(input.PipelineName)
	return &awscodepipeline.ListPipelineExecutionsOutput{
		PipelineExecutionSummaries: f.executionsByPipeline[name],
	}, nil
}

func (f *fakeCodePipelineAPI) ListWebhooks(
	context.Context,
	*awscodepipeline.ListWebhooksInput,
	...func(*awscodepipeline.Options),
) (*awscodepipeline.ListWebhooksOutput, error) {
	return &awscodepipeline.ListWebhooksOutput{Webhooks: f.webhooks}, nil
}

func (f *fakeCodePipelineAPI) ListActionTypes(
	context.Context,
	*awscodepipeline.ListActionTypesInput,
	...func(*awscodepipeline.Options),
) (*awscodepipeline.ListActionTypesOutput, error) {
	return &awscodepipeline.ListActionTypesOutput{ActionTypes: f.actionTypes}, nil
}

func (f *fakeCodePipelineAPI) ListTagsForResource(
	_ context.Context,
	input *awscodepipeline.ListTagsForResourceInput,
	_ ...func(*awscodepipeline.Options),
) (*awscodepipeline.ListTagsForResourceOutput, error) {
	arn := aws.ToString(input.ResourceArn)
	tags := f.tags[arn]
	out := make([]cptypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, cptypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return &awscodepipeline.ListTagsForResourceOutput{Tags: out}, nil
}

var (
	_ apiClient           = (*fakeCodePipelineAPI)(nil)
	_ codepipeline.Client = (*Client)(nil)
)
