// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsimagebuilder "github.com/aws/aws-sdk-go-v2/service/imagebuilder"
	awsimagebuildertypes "github.com/aws/aws-sdk-go-v2/service/imagebuilder/types"

	imagebuilderservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/imagebuilder"
)

// listPipelines pages ListImagePipelines to exhaustion. The list summary already
// carries every pipeline field the scanner needs (recipe and configuration ARNs,
// status, schedule), so no per-pipeline get call is made.
func (c *Client) listPipelines(ctx context.Context) ([]imagebuilderservice.ImagePipeline, error) {
	var pipelines []imagebuilderservice.ImagePipeline
	var nextToken *string
	for {
		var page *awsimagebuilder.ListImagePipelinesOutput
		err := c.recordAPICall(ctx, "ListImagePipelines", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListImagePipelines(callCtx, &awsimagebuilder.ListImagePipelinesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return pipelines, nil
		}
		for i := range page.ImagePipelineList {
			pipelines = append(pipelines, mapPipeline(page.ImagePipelineList[i]))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return pipelines, nil
		}
	}
}

// listImageRecipes pages ListImageRecipes (scoped to Self-owned recipes) and
// fetches the full recipe for each summary so component ARNs and the parent
// image are available. A recipe that disappears between list and get is skipped.
func (c *Client) listImageRecipes(ctx context.Context) ([]imagebuilderservice.ImageRecipe, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsimagebuilder.ListImageRecipesOutput
		err := c.recordAPICall(ctx, "ListImageRecipes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListImageRecipes(callCtx, &awsimagebuilder.ListImageRecipesInput{
				Owner:     awsimagebuildertypes.OwnershipSelf,
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for i := range page.ImageRecipeSummaryList {
			if arn := strings.TrimSpace(aws.ToString(page.ImageRecipeSummaryList[i].Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	recipes := make([]imagebuilderservice.ImageRecipe, 0, len(arns))
	for _, arn := range arns {
		recipe, err := c.getImageRecipe(ctx, arn)
		if err != nil {
			return nil, err
		}
		if recipe != nil {
			recipes = append(recipes, *recipe)
		}
	}
	return recipes, nil
}

func (c *Client) getImageRecipe(ctx context.Context, arn string) (*imagebuilderservice.ImageRecipe, error) {
	var out *awsimagebuilder.GetImageRecipeOutput
	err := c.recordAPICall(ctx, "GetImageRecipe", func(callCtx context.Context) error {
		var callErr error
		out, callErr = c.client.GetImageRecipe(callCtx, &awsimagebuilder.GetImageRecipeInput{
			ImageRecipeArn: aws.String(arn),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.ImageRecipe == nil {
		return nil, nil
	}
	recipe := mapImageRecipe(*out.ImageRecipe)
	return &recipe, nil
}

// listContainerRecipes pages ListContainerRecipes (scoped to Self-owned recipes)
// and fetches the full recipe for each summary so the target ECR repository, KMS
// key, and component ARNs are available.
func (c *Client) listContainerRecipes(ctx context.Context) ([]imagebuilderservice.ContainerRecipe, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsimagebuilder.ListContainerRecipesOutput
		err := c.recordAPICall(ctx, "ListContainerRecipes", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListContainerRecipes(callCtx, &awsimagebuilder.ListContainerRecipesInput{
				Owner:     awsimagebuildertypes.OwnershipSelf,
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for i := range page.ContainerRecipeSummaryList {
			if arn := strings.TrimSpace(aws.ToString(page.ContainerRecipeSummaryList[i].Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	recipes := make([]imagebuilderservice.ContainerRecipe, 0, len(arns))
	for _, arn := range arns {
		recipe, err := c.getContainerRecipe(ctx, arn)
		if err != nil {
			return nil, err
		}
		if recipe != nil {
			recipes = append(recipes, *recipe)
		}
	}
	return recipes, nil
}

func (c *Client) getContainerRecipe(ctx context.Context, arn string) (*imagebuilderservice.ContainerRecipe, error) {
	var out *awsimagebuilder.GetContainerRecipeOutput
	err := c.recordAPICall(ctx, "GetContainerRecipe", func(callCtx context.Context) error {
		var callErr error
		out, callErr = c.client.GetContainerRecipe(callCtx, &awsimagebuilder.GetContainerRecipeInput{
			ContainerRecipeArn: aws.String(arn),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.ContainerRecipe == nil {
		return nil, nil
	}
	recipe := mapContainerRecipe(*out.ContainerRecipe)
	return &recipe, nil
}

// listInfrastructureConfigurations pages the summary list and fetches the full
// configuration for each so the instance profile, subnet, security groups, SNS
// topic, and S3 logging location are available.
func (c *Client) listInfrastructureConfigurations(ctx context.Context) ([]imagebuilderservice.InfrastructureConfiguration, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsimagebuilder.ListInfrastructureConfigurationsOutput
		err := c.recordAPICall(ctx, "ListInfrastructureConfigurations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListInfrastructureConfigurations(callCtx, &awsimagebuilder.ListInfrastructureConfigurationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for i := range page.InfrastructureConfigurationSummaryList {
			if arn := strings.TrimSpace(aws.ToString(page.InfrastructureConfigurationSummaryList[i].Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	configs := make([]imagebuilderservice.InfrastructureConfiguration, 0, len(arns))
	for _, arn := range arns {
		config, err := c.getInfrastructureConfiguration(ctx, arn)
		if err != nil {
			return nil, err
		}
		if config != nil {
			configs = append(configs, *config)
		}
	}
	return configs, nil
}

func (c *Client) getInfrastructureConfiguration(ctx context.Context, arn string) (*imagebuilderservice.InfrastructureConfiguration, error) {
	var out *awsimagebuilder.GetInfrastructureConfigurationOutput
	err := c.recordAPICall(ctx, "GetInfrastructureConfiguration", func(callCtx context.Context) error {
		var callErr error
		out, callErr = c.client.GetInfrastructureConfiguration(callCtx, &awsimagebuilder.GetInfrastructureConfigurationInput{
			InfrastructureConfigurationArn: aws.String(arn),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.InfrastructureConfiguration == nil {
		return nil, nil
	}
	config := mapInfrastructureConfiguration(*out.InfrastructureConfiguration)
	return &config, nil
}

// listDistributionConfigurations pages the summary list and fetches the full
// configuration for each so the distribution target regions are available.
func (c *Client) listDistributionConfigurations(ctx context.Context) ([]imagebuilderservice.DistributionConfiguration, error) {
	var arns []string
	var nextToken *string
	for {
		var page *awsimagebuilder.ListDistributionConfigurationsOutput
		err := c.recordAPICall(ctx, "ListDistributionConfigurations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListDistributionConfigurations(callCtx, &awsimagebuilder.ListDistributionConfigurationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for i := range page.DistributionConfigurationSummaryList {
			if arn := strings.TrimSpace(aws.ToString(page.DistributionConfigurationSummaryList[i].Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	configs := make([]imagebuilderservice.DistributionConfiguration, 0, len(arns))
	for _, arn := range arns {
		config, err := c.getDistributionConfiguration(ctx, arn)
		if err != nil {
			return nil, err
		}
		if config != nil {
			configs = append(configs, *config)
		}
	}
	return configs, nil
}

func (c *Client) getDistributionConfiguration(ctx context.Context, arn string) (*imagebuilderservice.DistributionConfiguration, error) {
	var out *awsimagebuilder.GetDistributionConfigurationOutput
	err := c.recordAPICall(ctx, "GetDistributionConfiguration", func(callCtx context.Context) error {
		var callErr error
		out, callErr = c.client.GetDistributionConfiguration(callCtx, &awsimagebuilder.GetDistributionConfigurationInput{
			DistributionConfigurationArn: aws.String(arn),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.DistributionConfiguration == nil {
		return nil, nil
	}
	config := mapDistributionConfiguration(*out.DistributionConfiguration)
	return &config, nil
}
