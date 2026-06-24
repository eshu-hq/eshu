// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"
	awssagemakertypes "github.com/aws/aws-sdk-go-v2/service/sagemaker/types"

	smservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sagemaker"
)

// ListNotebookInstances returns notebook-instance metadata. Subnet placement
// and lifecycle-config name come from DescribeNotebookInstance; the
// lifecycle-config script body is never read.
func (c *Client) ListNotebookInstances(ctx context.Context) ([]smservice.NotebookInstance, error) {
	paginator := awssagemaker.NewListNotebookInstancesPaginator(c.client, &awssagemaker.ListNotebookInstancesInput{})
	var notebooks []smservice.NotebookInstance
	for paginator.HasMorePages() {
		var page *awssagemaker.ListNotebookInstancesOutput
		if err := c.page(ctx, "ListNotebookInstances", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.NotebookInstances {
			notebook := smservice.NotebookInstance{
				ARN:                 aws.ToString(summary.NotebookInstanceArn),
				Name:                aws.ToString(summary.NotebookInstanceName),
				Status:              string(summary.NotebookInstanceStatus),
				InstanceType:        string(summary.InstanceType),
				LifecycleConfigName: aws.ToString(summary.NotebookInstanceLifecycleConfigName),
				CreationTime:        aws.ToTime(summary.CreationTime),
				LastModifiedTime:    aws.ToTime(summary.LastModifiedTime),
			}
			if err := c.enrichNotebook(ctx, &notebook); err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, notebook.ARN)
			if err != nil {
				return nil, err
			}
			notebook.Tags = tags
			notebooks = append(notebooks, notebook)
		}
	}
	return notebooks, nil
}

// enrichNotebook reads VPC placement and access metadata from
// DescribeNotebookInstance. It deliberately ignores any lifecycle-config script
// body, which DescribeNotebookInstance does not expose.
func (c *Client) enrichNotebook(ctx context.Context, notebook *smservice.NotebookInstance) error {
	name := strings.TrimSpace(notebook.Name)
	if name == "" {
		return nil
	}
	var output *awssagemaker.DescribeNotebookInstanceOutput
	if err := c.page(ctx, "DescribeNotebookInstance", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeNotebookInstance(callCtx, &awssagemaker.DescribeNotebookInstanceInput{
			NotebookInstanceName: aws.String(name),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	notebook.SubnetID = aws.ToString(output.SubnetId)
	notebook.SecurityGroupIDs = append([]string(nil), output.SecurityGroups...)
	notebook.DirectInternetAccess = string(output.DirectInternetAccess)
	notebook.PlatformIdentifier = aws.ToString(output.PlatformIdentifier)
	if output.NotebookInstanceLifecycleConfigName != nil {
		notebook.LifecycleConfigName = aws.ToString(output.NotebookInstanceLifecycleConfigName)
	}
	return nil
}

// ListModels returns model metadata with container image and S3 artifact
// references. Container Environment maps are never read into scanner state.
func (c *Client) ListModels(ctx context.Context) ([]smservice.Model, error) {
	paginator := awssagemaker.NewListModelsPaginator(c.client, &awssagemaker.ListModelsInput{})
	var models []smservice.Model
	for paginator.HasMorePages() {
		var page *awssagemaker.ListModelsOutput
		if err := c.page(ctx, "ListModels", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.Models {
			model := smservice.Model{
				ARN:          aws.ToString(summary.ModelArn),
				Name:         aws.ToString(summary.ModelName),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			if err := c.enrichModel(ctx, &model); err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, model.ARN)
			if err != nil {
				return nil, err
			}
			model.Tags = tags
			models = append(models, model)
		}
	}
	return models, nil
}

// enrichModel reads the execution role, VPC subnets, and per-container image
// and artifact references from DescribeModel. Container Environment maps are
// intentionally dropped because they can carry secret-like values.
func (c *Client) enrichModel(ctx context.Context, model *smservice.Model) error {
	name := strings.TrimSpace(model.Name)
	if name == "" {
		return nil
	}
	var output *awssagemaker.DescribeModelOutput
	if err := c.page(ctx, "DescribeModel", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeModel(callCtx, &awssagemaker.DescribeModelInput{
			ModelName: aws.String(name),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	model.ExecutionRole = aws.ToString(output.ExecutionRoleArn)
	model.NetworkIsolated = aws.ToBool(output.EnableNetworkIsolation)
	if output.VpcConfig != nil {
		model.VPCSubnetIDs = append([]string(nil), output.VpcConfig.Subnets...)
	}
	model.Containers = modelContainers(output.PrimaryContainer, output.Containers)
	return nil
}

// modelContainers flattens the primary container and inference-pipeline
// containers into scanner-owned image and artifact references. It reads only
// Image and ModelDataUrl; it never reads the container Environment map.
func modelContainers(
	primary *awssagemakertypes.ContainerDefinition,
	pipeline []awssagemakertypes.ContainerDefinition,
) []smservice.ModelContainer {
	var containers []smservice.ModelContainer
	add := func(definition *awssagemakertypes.ContainerDefinition) {
		if definition == nil {
			return
		}
		container := smservice.ModelContainer{
			Image:        aws.ToString(definition.Image),
			ModelDataURL: aws.ToString(definition.ModelDataUrl),
		}
		if container.Image == "" && container.ModelDataURL == "" {
			return
		}
		containers = append(containers, container)
	}
	add(primary)
	for i := range pipeline {
		add(&pipeline[i])
	}
	return containers
}

// ListEndpoints returns endpoint metadata with the active endpoint
// configuration name from DescribeEndpoint.
func (c *Client) ListEndpoints(ctx context.Context) ([]smservice.Endpoint, error) {
	paginator := awssagemaker.NewListEndpointsPaginator(c.client, &awssagemaker.ListEndpointsInput{})
	var endpoints []smservice.Endpoint
	for paginator.HasMorePages() {
		var page *awssagemaker.ListEndpointsOutput
		if err := c.page(ctx, "ListEndpoints", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.Endpoints {
			endpoint := smservice.Endpoint{
				ARN:              aws.ToString(summary.EndpointArn),
				Name:             aws.ToString(summary.EndpointName),
				Status:           string(summary.EndpointStatus),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
			}
			config, err := c.endpointConfigName(ctx, endpoint.Name)
			if err != nil {
				return nil, err
			}
			endpoint.EndpointConfig = config
			tags, err := c.listTags(ctx, endpoint.ARN)
			if err != nil {
				return nil, err
			}
			endpoint.Tags = tags
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints, nil
}

func (c *Client) endpointConfigName(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	var output *awssagemaker.DescribeEndpointOutput
	if err := c.page(ctx, "DescribeEndpoint", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeEndpoint(callCtx, &awssagemaker.DescribeEndpointInput{
			EndpointName: aws.String(name),
		})
		return err
	}); err != nil {
		return "", err
	}
	if output == nil {
		return "", nil
	}
	return aws.ToString(output.EndpointConfigName), nil
}

// ListEndpointConfigs returns endpoint-configuration metadata with the
// production-variant model names read from DescribeEndpointConfig.
func (c *Client) ListEndpointConfigs(ctx context.Context) ([]smservice.EndpointConfig, error) {
	paginator := awssagemaker.NewListEndpointConfigsPaginator(c.client, &awssagemaker.ListEndpointConfigsInput{})
	var configs []smservice.EndpointConfig
	for paginator.HasMorePages() {
		var page *awssagemaker.ListEndpointConfigsOutput
		if err := c.page(ctx, "ListEndpointConfigs", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.EndpointConfigs {
			config := smservice.EndpointConfig{
				ARN:          aws.ToString(summary.EndpointConfigArn),
				Name:         aws.ToString(summary.EndpointConfigName),
				CreationTime: aws.ToTime(summary.CreationTime),
			}
			if err := c.enrichEndpointConfig(ctx, &config); err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, config.ARN)
			if err != nil {
				return nil, err
			}
			config.Tags = tags
			configs = append(configs, config)
		}
	}
	return configs, nil
}

func (c *Client) enrichEndpointConfig(ctx context.Context, config *smservice.EndpointConfig) error {
	name := strings.TrimSpace(config.Name)
	if name == "" {
		return nil
	}
	var output *awssagemaker.DescribeEndpointConfigOutput
	if err := c.page(ctx, "DescribeEndpointConfig", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeEndpointConfig(callCtx, &awssagemaker.DescribeEndpointConfigInput{
			EndpointConfigName: aws.String(name),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	config.KMSKeyID = aws.ToString(output.KmsKeyId)
	for _, variant := range output.ProductionVariants {
		if model := strings.TrimSpace(aws.ToString(variant.ModelName)); model != "" {
			config.ModelNames = append(config.ModelNames, model)
		}
	}
	return nil
}
