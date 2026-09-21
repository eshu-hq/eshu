// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"
	awssagemakertypes "github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

// fakeSageMakerAPI is a single-page apiClient double. Each List returns its
// seeded slice once; each Describe returns its seeded output. calledOps records
// every operation invoked so tests can assert a forbidden Describe was never
// reached.
type fakeSageMakerAPI struct {
	notebooks       []awssagemakertypes.NotebookInstanceSummary
	models          []awssagemakertypes.ModelSummary
	endpoints       []awssagemakertypes.EndpointSummary
	endpointConfigs []awssagemakertypes.EndpointConfigSummary
	trainingJobs    []awssagemakertypes.TrainingJobSummary
	processingJobs  []awssagemakertypes.ProcessingJobSummary
	transformJobs   []awssagemakertypes.TransformJobSummary
	tuningJobs      []awssagemakertypes.HyperParameterTuningJobSummary
	projects        []awssagemakertypes.ProjectSummary
	pipelines       []awssagemakertypes.PipelineSummary
	featureGroups   []awssagemakertypes.FeatureGroupSummary
	domains         []awssagemakertypes.DomainDetails
	userProfiles    []awssagemakertypes.UserProfileDetails
	apps            []awssagemakertypes.AppDetails
	inferenceComps  []awssagemakertypes.InferenceComponentSummary
	tags            []awssagemakertypes.Tag

	describeNotebook       *awssagemaker.DescribeNotebookInstanceOutput
	describeModel          *awssagemaker.DescribeModelOutput
	describeEndpoint       *awssagemaker.DescribeEndpointOutput
	describeEndpointConfig *awssagemaker.DescribeEndpointConfigOutput
	describeTrainingJob    *awssagemaker.DescribeTrainingJobOutput
	describeDomain         *awssagemaker.DescribeDomainOutput

	calledOps map[string]bool
}

func (f *fakeSageMakerAPI) record(op string) {
	if f.calledOps == nil {
		f.calledOps = map[string]bool{}
	}
	f.calledOps[op] = true
}

func (f *fakeSageMakerAPI) ListNotebookInstances(_ context.Context, _ *awssagemaker.ListNotebookInstancesInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListNotebookInstancesOutput, error) {
	f.record("ListNotebookInstances")
	return &awssagemaker.ListNotebookInstancesOutput{NotebookInstances: f.notebooks}, nil
}

func (f *fakeSageMakerAPI) DescribeNotebookInstance(_ context.Context, _ *awssagemaker.DescribeNotebookInstanceInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeNotebookInstanceOutput, error) {
	f.record("DescribeNotebookInstance")
	return f.describeNotebook, nil
}

func (f *fakeSageMakerAPI) ListModels(_ context.Context, _ *awssagemaker.ListModelsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListModelsOutput, error) {
	f.record("ListModels")
	return &awssagemaker.ListModelsOutput{Models: f.models}, nil
}

func (f *fakeSageMakerAPI) DescribeModel(_ context.Context, _ *awssagemaker.DescribeModelInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeModelOutput, error) {
	f.record("DescribeModel")
	return f.describeModel, nil
}

func (f *fakeSageMakerAPI) ListEndpoints(_ context.Context, _ *awssagemaker.ListEndpointsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListEndpointsOutput, error) {
	f.record("ListEndpoints")
	return &awssagemaker.ListEndpointsOutput{Endpoints: f.endpoints}, nil
}

func (f *fakeSageMakerAPI) DescribeEndpoint(_ context.Context, _ *awssagemaker.DescribeEndpointInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeEndpointOutput, error) {
	f.record("DescribeEndpoint")
	return f.describeEndpoint, nil
}

func (f *fakeSageMakerAPI) ListEndpointConfigs(_ context.Context, _ *awssagemaker.ListEndpointConfigsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListEndpointConfigsOutput, error) {
	f.record("ListEndpointConfigs")
	return &awssagemaker.ListEndpointConfigsOutput{EndpointConfigs: f.endpointConfigs}, nil
}

func (f *fakeSageMakerAPI) DescribeEndpointConfig(_ context.Context, _ *awssagemaker.DescribeEndpointConfigInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeEndpointConfigOutput, error) {
	f.record("DescribeEndpointConfig")
	return f.describeEndpointConfig, nil
}

func (f *fakeSageMakerAPI) ListTrainingJobs(_ context.Context, _ *awssagemaker.ListTrainingJobsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListTrainingJobsOutput, error) {
	f.record("ListTrainingJobs")
	return &awssagemaker.ListTrainingJobsOutput{TrainingJobSummaries: f.trainingJobs}, nil
}

func (f *fakeSageMakerAPI) DescribeTrainingJob(_ context.Context, _ *awssagemaker.DescribeTrainingJobInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeTrainingJobOutput, error) {
	f.record("DescribeTrainingJob")
	return f.describeTrainingJob, nil
}

func (f *fakeSageMakerAPI) ListProcessingJobs(_ context.Context, _ *awssagemaker.ListProcessingJobsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListProcessingJobsOutput, error) {
	f.record("ListProcessingJobs")
	return &awssagemaker.ListProcessingJobsOutput{ProcessingJobSummaries: f.processingJobs}, nil
}

func (f *fakeSageMakerAPI) ListTransformJobs(_ context.Context, _ *awssagemaker.ListTransformJobsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListTransformJobsOutput, error) {
	f.record("ListTransformJobs")
	return &awssagemaker.ListTransformJobsOutput{TransformJobSummaries: f.transformJobs}, nil
}

func (f *fakeSageMakerAPI) ListHyperParameterTuningJobs(_ context.Context, _ *awssagemaker.ListHyperParameterTuningJobsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListHyperParameterTuningJobsOutput, error) {
	f.record("ListHyperParameterTuningJobs")
	return &awssagemaker.ListHyperParameterTuningJobsOutput{HyperParameterTuningJobSummaries: f.tuningJobs}, nil
}

func (f *fakeSageMakerAPI) ListProjects(_ context.Context, _ *awssagemaker.ListProjectsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListProjectsOutput, error) {
	f.record("ListProjects")
	return &awssagemaker.ListProjectsOutput{ProjectSummaryList: f.projects}, nil
}

func (f *fakeSageMakerAPI) ListPipelines(_ context.Context, _ *awssagemaker.ListPipelinesInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListPipelinesOutput, error) {
	f.record("ListPipelines")
	return &awssagemaker.ListPipelinesOutput{PipelineSummaries: f.pipelines}, nil
}

func (f *fakeSageMakerAPI) ListFeatureGroups(_ context.Context, _ *awssagemaker.ListFeatureGroupsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListFeatureGroupsOutput, error) {
	f.record("ListFeatureGroups")
	return &awssagemaker.ListFeatureGroupsOutput{FeatureGroupSummaries: f.featureGroups}, nil
}

func (f *fakeSageMakerAPI) ListDomains(_ context.Context, _ *awssagemaker.ListDomainsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListDomainsOutput, error) {
	f.record("ListDomains")
	return &awssagemaker.ListDomainsOutput{Domains: f.domains}, nil
}

func (f *fakeSageMakerAPI) DescribeDomain(_ context.Context, _ *awssagemaker.DescribeDomainInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.DescribeDomainOutput, error) {
	f.record("DescribeDomain")
	return f.describeDomain, nil
}

func (f *fakeSageMakerAPI) ListUserProfiles(_ context.Context, _ *awssagemaker.ListUserProfilesInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListUserProfilesOutput, error) {
	f.record("ListUserProfiles")
	return &awssagemaker.ListUserProfilesOutput{UserProfiles: f.userProfiles}, nil
}

func (f *fakeSageMakerAPI) ListApps(_ context.Context, _ *awssagemaker.ListAppsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListAppsOutput, error) {
	f.record("ListApps")
	return &awssagemaker.ListAppsOutput{Apps: f.apps}, nil
}

func (f *fakeSageMakerAPI) ListInferenceComponents(_ context.Context, _ *awssagemaker.ListInferenceComponentsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListInferenceComponentsOutput, error) {
	f.record("ListInferenceComponents")
	return &awssagemaker.ListInferenceComponentsOutput{InferenceComponents: f.inferenceComps}, nil
}

func (f *fakeSageMakerAPI) ListTags(_ context.Context, _ *awssagemaker.ListTagsInput, _ ...func(*awssagemaker.Options)) (*awssagemaker.ListTagsOutput, error) {
	f.record("ListTags")
	return &awssagemaker.ListTagsOutput{Tags: f.tags}, nil
}

var _ apiClient = (*fakeSageMakerAPI)(nil)
