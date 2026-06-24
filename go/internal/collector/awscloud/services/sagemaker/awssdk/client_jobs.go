// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"

	smservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sagemaker"
)

// ListTrainingJobs returns training-job metadata. The execution role ARN is
// read from DescribeTrainingJob; hyperparameter values and data references are
// never requested into scanner-owned types.
func (c *Client) ListTrainingJobs(ctx context.Context) ([]smservice.TrainingJob, error) {
	paginator := awssagemaker.NewListTrainingJobsPaginator(c.client, &awssagemaker.ListTrainingJobsInput{})
	var jobs []smservice.TrainingJob
	for paginator.HasMorePages() {
		var page *awssagemaker.ListTrainingJobsOutput
		if err := c.page(ctx, "ListTrainingJobs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.TrainingJobSummaries {
			job := smservice.TrainingJob{
				ARN:              aws.ToString(summary.TrainingJobArn),
				Name:             aws.ToString(summary.TrainingJobName),
				Status:           string(summary.TrainingJobStatus),
				SecondaryStatus:  string(summary.SecondaryStatus),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
				TrainingEndTime:  aws.ToTime(summary.TrainingEndTime),
			}
			role, err := c.trainingJobRole(ctx, aws.ToString(summary.TrainingJobName))
			if err != nil {
				return nil, err
			}
			job.ExecutionRole = role
			tags, err := c.listTags(ctx, job.ARN)
			if err != nil {
				return nil, err
			}
			job.Tags = tags
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// trainingJobRole reads only the execution role ARN from DescribeTrainingJob.
// HyperParameters, InputDataConfig, and OutputDataConfig in the response are
// never copied into scanner-owned state.
func (c *Client) trainingJobRole(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	var output *awssagemaker.DescribeTrainingJobOutput
	if err := c.page(ctx, "DescribeTrainingJob", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeTrainingJob(callCtx, &awssagemaker.DescribeTrainingJobInput{
			TrainingJobName: aws.String(name),
		})
		return err
	}); err != nil {
		return "", err
	}
	if output == nil {
		return "", nil
	}
	return aws.ToString(output.RoleArn), nil
}

// ListProcessingJobs returns processing-job metadata from the list summary.
// Processing input/output data references are never read.
func (c *Client) ListProcessingJobs(ctx context.Context) ([]smservice.ProcessingJob, error) {
	paginator := awssagemaker.NewListProcessingJobsPaginator(c.client, &awssagemaker.ListProcessingJobsInput{})
	var jobs []smservice.ProcessingJob
	for paginator.HasMorePages() {
		var page *awssagemaker.ListProcessingJobsOutput
		if err := c.page(ctx, "ListProcessingJobs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.ProcessingJobSummaries {
			job := smservice.ProcessingJob{
				ARN:              aws.ToString(summary.ProcessingJobArn),
				Name:             aws.ToString(summary.ProcessingJobName),
				Status:           string(summary.ProcessingJobStatus),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
				ProcessingEnd:    aws.ToTime(summary.ProcessingEndTime),
			}
			tags, err := c.listTags(ctx, job.ARN)
			if err != nil {
				return nil, err
			}
			job.Tags = tags
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// ListTransformJobs returns batch-transform-job metadata from the list summary.
// Transform input/output data references are never read.
func (c *Client) ListTransformJobs(ctx context.Context) ([]smservice.TransformJob, error) {
	paginator := awssagemaker.NewListTransformJobsPaginator(c.client, &awssagemaker.ListTransformJobsInput{})
	var jobs []smservice.TransformJob
	for paginator.HasMorePages() {
		var page *awssagemaker.ListTransformJobsOutput
		if err := c.page(ctx, "ListTransformJobs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.TransformJobSummaries {
			job := smservice.TransformJob{
				ARN:              aws.ToString(summary.TransformJobArn),
				Name:             aws.ToString(summary.TransformJobName),
				Status:           string(summary.TransformJobStatus),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
				TransformEnd:     aws.ToTime(summary.TransformEndTime),
			}
			tags, err := c.listTags(ctx, job.ARN)
			if err != nil {
				return nil, err
			}
			job.Tags = tags
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// ListHyperParameterTuningJobs returns tuning-job metadata from the list
// summary. Tuned hyperparameter values are never read.
func (c *Client) ListHyperParameterTuningJobs(ctx context.Context) ([]smservice.HyperParameterTuningJob, error) {
	paginator := awssagemaker.NewListHyperParameterTuningJobsPaginator(c.client, &awssagemaker.ListHyperParameterTuningJobsInput{})
	var jobs []smservice.HyperParameterTuningJob
	for paginator.HasMorePages() {
		var page *awssagemaker.ListHyperParameterTuningJobsOutput
		if err := c.page(ctx, "ListHyperParameterTuningJobs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.HyperParameterTuningJobSummaries {
			job := smservice.HyperParameterTuningJob{
				ARN:              aws.ToString(summary.HyperParameterTuningJobArn),
				Name:             aws.ToString(summary.HyperParameterTuningJobName),
				Status:           string(summary.HyperParameterTuningJobStatus),
				Strategy:         string(summary.Strategy),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
			}
			tags, err := c.listTags(ctx, job.ARN)
			if err != nil {
				return nil, err
			}
			job.Tags = tags
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// ListProjects returns SageMaker project metadata from the list summary.
func (c *Client) ListProjects(ctx context.Context) ([]smservice.Project, error) {
	paginator := awssagemaker.NewListProjectsPaginator(c.client, &awssagemaker.ListProjectsInput{})
	var projects []smservice.Project
	for paginator.HasMorePages() {
		var page *awssagemaker.ListProjectsOutput
		if err := c.page(ctx, "ListProjects", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.ProjectSummaryList {
			project := smservice.Project{
				ARN:          aws.ToString(summary.ProjectArn),
				ID:           aws.ToString(summary.ProjectId),
				Name:         aws.ToString(summary.ProjectName),
				Status:       string(summary.ProjectStatus),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			tags, err := c.listTags(ctx, project.ARN)
			if err != nil {
				return nil, err
			}
			project.Tags = tags
			projects = append(projects, project)
		}
	}
	return projects, nil
}

// ListPipelines returns pipeline metadata from the list summary. The pipeline
// definition body is never read or persisted.
func (c *Client) ListPipelines(ctx context.Context) ([]smservice.Pipeline, error) {
	paginator := awssagemaker.NewListPipelinesPaginator(c.client, &awssagemaker.ListPipelinesInput{})
	var pipelines []smservice.Pipeline
	for paginator.HasMorePages() {
		var page *awssagemaker.ListPipelinesOutput
		if err := c.page(ctx, "ListPipelines", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.PipelineSummaries {
			pipeline := smservice.Pipeline{
				ARN:              aws.ToString(summary.PipelineArn),
				Name:             aws.ToString(summary.PipelineName),
				DisplayName:      aws.ToString(summary.PipelineDisplayName),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
				LastExecution:    aws.ToTime(summary.LastExecutionTime),
			}
			tags, err := c.listTags(ctx, pipeline.ARN)
			if err != nil {
				return nil, err
			}
			pipeline.Tags = tags
			pipelines = append(pipelines, pipeline)
		}
	}
	return pipelines, nil
}

// ListFeatureGroups returns feature-group metadata from the list summary.
// Feature record contents are never read.
func (c *Client) ListFeatureGroups(ctx context.Context) ([]smservice.FeatureGroup, error) {
	paginator := awssagemaker.NewListFeatureGroupsPaginator(c.client, &awssagemaker.ListFeatureGroupsInput{})
	var groups []smservice.FeatureGroup
	for paginator.HasMorePages() {
		var page *awssagemaker.ListFeatureGroupsOutput
		if err := c.page(ctx, "ListFeatureGroups", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.FeatureGroupSummaries {
			group := smservice.FeatureGroup{
				ARN:          aws.ToString(summary.FeatureGroupArn),
				Name:         aws.ToString(summary.FeatureGroupName),
				Status:       string(summary.FeatureGroupStatus),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			if summary.OfflineStoreStatus != nil {
				group.OfflineStore = string(summary.OfflineStoreStatus.Status)
			}
			tags, err := c.listTags(ctx, group.ARN)
			if err != nil {
				return nil, err
			}
			group.Tags = tags
			groups = append(groups, group)
		}
	}
	return groups, nil
}

// page wraps a single paginated/Describe call in recordAPICall so every call
// site stays terse while keeping the telemetry contract.
func (c *Client) page(ctx context.Context, operation string, call func(context.Context) error) error {
	return c.recordAPICall(ctx, operation, call)
}
