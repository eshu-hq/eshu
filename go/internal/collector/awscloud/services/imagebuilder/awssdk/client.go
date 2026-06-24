// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsimagebuilder "github.com/aws/aws-sdk-go-v2/service/imagebuilder"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	imagebuilderservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/imagebuilder"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS EC2 Image Builder API the
// adapter calls. It is deliberately limited to the list reads that enumerate
// pipelines, recipes, container recipes, and configurations and the matching get
// reads that fetch one resource's control-plane metadata. It exposes no
// Create/Update/Delete mutation, no Start/Import/Cancel run control, and none of
// the component build-version, image build-version, scan-finding, or workflow
// reads, so the adapter cannot read or persist component build-document bodies,
// Dockerfile bodies, user data, scan findings, or build artifacts. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	ListImagePipelines(
		context.Context,
		*awsimagebuilder.ListImagePipelinesInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.ListImagePipelinesOutput, error)
	ListImageRecipes(
		context.Context,
		*awsimagebuilder.ListImageRecipesInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.ListImageRecipesOutput, error)
	GetImageRecipe(
		context.Context,
		*awsimagebuilder.GetImageRecipeInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.GetImageRecipeOutput, error)
	ListContainerRecipes(
		context.Context,
		*awsimagebuilder.ListContainerRecipesInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.ListContainerRecipesOutput, error)
	GetContainerRecipe(
		context.Context,
		*awsimagebuilder.GetContainerRecipeInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.GetContainerRecipeOutput, error)
	ListInfrastructureConfigurations(
		context.Context,
		*awsimagebuilder.ListInfrastructureConfigurationsInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.ListInfrastructureConfigurationsOutput, error)
	GetInfrastructureConfiguration(
		context.Context,
		*awsimagebuilder.GetInfrastructureConfigurationInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.GetInfrastructureConfigurationOutput, error)
	ListDistributionConfigurations(
		context.Context,
		*awsimagebuilder.ListDistributionConfigurationsInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.ListDistributionConfigurationsOutput, error)
	GetDistributionConfiguration(
		context.Context,
		*awsimagebuilder.GetDistributionConfigurationInput,
		...func(*awsimagebuilder.Options),
	) (*awsimagebuilder.GetDistributionConfigurationOutput, error)
}

// Client adapts AWS SDK EC2 Image Builder control-plane calls into scanner-owned
// metadata. It never reads component build-document bodies, Dockerfile bodies,
// instance user data, scan findings, or build artifacts, and never calls a
// mutation or run-control API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Image Builder SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsimagebuilder.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Image Builder pipeline, recipe, container recipe,
// infrastructure configuration, and distribution configuration metadata visible
// to the configured AWS credentials. Recipes and container recipes are scoped to
// account-owned (Self) resources so Amazon-managed and shared catalog resources
// do not flood the scan. Component build-document bodies, Dockerfile bodies, and
// build artifacts are never read.
func (c *Client) Snapshot(ctx context.Context) (imagebuilderservice.Snapshot, error) {
	pipelines, err := c.listPipelines(ctx)
	if err != nil {
		return imagebuilderservice.Snapshot{}, err
	}
	imageRecipes, err := c.listImageRecipes(ctx)
	if err != nil {
		return imagebuilderservice.Snapshot{}, err
	}
	containerRecipes, err := c.listContainerRecipes(ctx)
	if err != nil {
		return imagebuilderservice.Snapshot{}, err
	}
	infraConfigs, err := c.listInfrastructureConfigurations(ctx)
	if err != nil {
		return imagebuilderservice.Snapshot{}, err
	}
	distConfigs, err := c.listDistributionConfigurations(ctx)
	if err != nil {
		return imagebuilderservice.Snapshot{}, err
	}
	return imagebuilderservice.Snapshot{
		Pipelines:                    pipelines,
		ImageRecipes:                 imageRecipes,
		ContainerRecipes:             containerRecipes,
		InfrastructureConfigurations: infraConfigs,
		DistributionConfigurations:   distConfigs,
	}, nil
}

var _ imagebuilderservice.Client = (*Client)(nil)

var _ apiClient = (*awsimagebuilder.Client)(nil)
