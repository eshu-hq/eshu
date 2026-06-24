// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbedrock "github.com/aws/aws-sdk-go-v2/service/bedrock"

	bedrocksvc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/bedrock"
)

// ListFoundationModels returns foundation model availability metadata from the
// read-only model list. ListFoundationModels is a single call (no paginator);
// it returns the full availability list in one response.
func (c *Client) ListFoundationModels(ctx context.Context) ([]bedrocksvc.FoundationModel, error) {
	var output *awsbedrock.ListFoundationModelsOutput
	if err := c.page(ctx, "ListFoundationModels", func(callCtx context.Context) (err error) {
		output, err = c.bedrock.ListFoundationModels(callCtx, &awsbedrock.ListFoundationModelsInput{})
		return err
	}); err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	models := make([]bedrocksvc.FoundationModel, 0, len(output.ModelSummaries))
	for _, summary := range output.ModelSummaries {
		model := bedrocksvc.FoundationModel{
			ARN:          aws.ToString(summary.ModelArn),
			ModelID:      aws.ToString(summary.ModelId),
			ProviderName: aws.ToString(summary.ProviderName),
		}
		if summary.ModelLifecycle != nil {
			model.LifecycleStatus = string(summary.ModelLifecycle.Status)
		}
		models = append(models, model)
	}
	return models, nil
}

// ListCustomModels returns custom model metadata. The base model id comes from
// the list summary; the customization job ARN and output S3 reference come from
// GetCustomModel. GetCustomModel also returns HyperParameters and the training
// data config, which the adapter deliberately never copies.
func (c *Client) ListCustomModels(ctx context.Context) ([]bedrocksvc.CustomModel, error) {
	paginator := awsbedrock.NewListCustomModelsPaginator(c.bedrock, &awsbedrock.ListCustomModelsInput{})
	var models []bedrocksvc.CustomModel
	for paginator.HasMorePages() {
		var page *awsbedrock.ListCustomModelsOutput
		if err := c.page(ctx, "ListCustomModels", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.ModelSummaries {
			model := bedrocksvc.CustomModel{
				ARN:          aws.ToString(summary.ModelArn),
				Name:         aws.ToString(summary.ModelName),
				BaseModelARN: aws.ToString(summary.BaseModelArn),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			if err := c.enrichCustomModel(ctx, &model); err != nil {
				return nil, err
			}
			tags, err := c.bedrockTags(ctx, model.ARN)
			if err != nil {
				return nil, err
			}
			model.Tags = tags
			models = append(models, model)
		}
	}
	return models, nil
}

// enrichCustomModel reads the customization job ARN and the output S3 reference
// from GetCustomModel. It deliberately ignores HyperParameters and the training
// data config (TrainingDataConfig.S3Uri); those are training-input/IP fields
// the scanner-owned CustomModel type has no place to hold.
func (c *Client) enrichCustomModel(ctx context.Context, model *bedrocksvc.CustomModel) error {
	identifier := strings.TrimSpace(model.ARN)
	if identifier == "" {
		identifier = strings.TrimSpace(model.Name)
	}
	if identifier == "" {
		return nil
	}
	var output *awsbedrock.GetCustomModelOutput
	if err := c.page(ctx, "GetCustomModel", func(callCtx context.Context) (err error) {
		output, err = c.bedrock.GetCustomModel(callCtx, &awsbedrock.GetCustomModelInput{
			ModelIdentifier: aws.String(identifier),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	if output.BaseModelArn != nil {
		model.BaseModelARN = aws.ToString(output.BaseModelArn)
	}
	model.JobARN = aws.ToString(output.JobArn)
	if output.OutputDataConfig != nil {
		model.OutputS3URI = aws.ToString(output.OutputDataConfig.S3Uri)
	}
	return nil
}

// ListModelCustomizationJobs returns model customization job metadata. The
// scanner copies job identity, status, and the base/custom model dependency
// only; it never reads hyperparameter values or training data references.
func (c *Client) ListModelCustomizationJobs(ctx context.Context) ([]bedrocksvc.ModelCustomizationJob, error) {
	paginator := awsbedrock.NewListModelCustomizationJobsPaginator(c.bedrock, &awsbedrock.ListModelCustomizationJobsInput{})
	var jobs []bedrocksvc.ModelCustomizationJob
	for paginator.HasMorePages() {
		var page *awsbedrock.ListModelCustomizationJobsOutput
		if err := c.page(ctx, "ListModelCustomizationJobs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.ModelCustomizationJobSummaries {
			job := bedrocksvc.ModelCustomizationJob{
				ARN:            aws.ToString(summary.JobArn),
				Name:           aws.ToString(summary.JobName),
				Status:         string(summary.Status),
				BaseModelARN:   aws.ToString(summary.BaseModelArn),
				CustomModelARN: aws.ToString(summary.CustomModelArn),
				CreationTime:   aws.ToTime(summary.CreationTime),
				EndTime:        aws.ToTime(summary.EndTime),
			}
			tags, err := c.bedrockTags(ctx, job.ARN)
			if err != nil {
				return nil, err
			}
			job.Tags = tags
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// ListProvisionedModelThroughputs returns provisioned model throughput
// metadata with the associated model ARN and allocated model units.
func (c *Client) ListProvisionedModelThroughputs(ctx context.Context) ([]bedrocksvc.ProvisionedModelThroughput, error) {
	paginator := awsbedrock.NewListProvisionedModelThroughputsPaginator(c.bedrock, &awsbedrock.ListProvisionedModelThroughputsInput{})
	var throughputs []bedrocksvc.ProvisionedModelThroughput
	for paginator.HasMorePages() {
		var page *awsbedrock.ListProvisionedModelThroughputsOutput
		if err := c.page(ctx, "ListProvisionedModelThroughputs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.ProvisionedModelSummaries {
			pt := bedrocksvc.ProvisionedModelThroughput{
				ARN:          aws.ToString(summary.ProvisionedModelArn),
				Name:         aws.ToString(summary.ProvisionedModelName),
				Status:       string(summary.Status),
				ModelARN:     aws.ToString(summary.ModelArn),
				ModelUnits:   aws.ToInt32(summary.ModelUnits),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			tags, err := c.bedrockTags(ctx, pt.ARN)
			if err != nil {
				return nil, err
			}
			pt.Tags = tags
			throughputs = append(throughputs, pt)
		}
	}
	return throughputs, nil
}

// ListGuardrails returns guardrail metadata. The scanner copies the name, id,
// version, and status only. The topic and content policy bodies are never read:
// the adapter never calls GetGuardrail, which is the only operation that returns
// the policy bodies, and the scanner-owned Guardrail type has no field for them.
func (c *Client) ListGuardrails(ctx context.Context) ([]bedrocksvc.Guardrail, error) {
	paginator := awsbedrock.NewListGuardrailsPaginator(c.bedrock, &awsbedrock.ListGuardrailsInput{})
	var guardrails []bedrocksvc.Guardrail
	for paginator.HasMorePages() {
		var page *awsbedrock.ListGuardrailsOutput
		if err := c.page(ctx, "ListGuardrails", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.Guardrails {
			guardrail := bedrocksvc.Guardrail{
				ARN:          aws.ToString(summary.Arn),
				ID:           aws.ToString(summary.Id),
				Name:         aws.ToString(summary.Name),
				Version:      aws.ToString(summary.Version),
				Status:       string(summary.Status),
				Description:  aws.ToString(summary.Description),
				CreationTime: aws.ToTime(summary.CreatedAt),
			}
			tags, err := c.bedrockTags(ctx, guardrail.ARN)
			if err != nil {
				return nil, err
			}
			guardrail.Tags = tags
			guardrails = append(guardrails, guardrail)
		}
	}
	return guardrails, nil
}
