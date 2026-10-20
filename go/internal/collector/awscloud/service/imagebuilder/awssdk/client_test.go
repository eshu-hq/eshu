// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsimagebuilder "github.com/aws/aws-sdk-go-v2/service/imagebuilder"
	awsimagebuildertypes "github.com/aws/aws-sdk-go-v2/service/imagebuilder/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsImageBuilderMetadataOnly(t *testing.T) {
	const (
		pipelineARN = "arn:aws:imagebuilder:us-east-1:123456789012:image-pipeline/web"
		recipeARN   = "arn:aws:imagebuilder:us-east-1:123456789012:image-recipe/web/1.0.0"
		contARN     = "arn:aws:imagebuilder:us-east-1:123456789012:container-recipe/api/2.0.0"
		infraARN    = "arn:aws:imagebuilder:us-east-1:123456789012:infrastructure-configuration/builders"
		distARN     = "arn:aws:imagebuilder:us-east-1:123456789012:distribution-configuration/multi"
	)

	api := &fakeImageBuilderAPI{
		pipelines: []awsimagebuildertypes.ImagePipeline{{
			Arn:                            aws.String(pipelineARN),
			Name:                           aws.String("web"),
			Status:                         awsimagebuildertypes.PipelineStatusEnabled,
			ImageRecipeArn:                 aws.String(recipeARN),
			InfrastructureConfigurationArn: aws.String(infraARN),
		}},
		imageRecipeSummaries: []awsimagebuildertypes.ImageRecipeSummary{{Arn: aws.String(recipeARN)}},
		imageRecipes: map[string]awsimagebuildertypes.ImageRecipe{recipeARN: {
			Arn:         aws.String(recipeARN),
			Name:        aws.String("web"),
			Platform:    awsimagebuildertypes.PlatformLinux,
			Version:     aws.String("1.0.0"),
			ParentImage: aws.String("ami-0123456789abcdef0"),
			Components: []awsimagebuildertypes.ComponentConfiguration{{
				ComponentArn: aws.String("arn:aws:imagebuilder:us-east-1:aws:component/update-linux/1.0.0"),
			}},
		}},
		containerRecipeSummaries: []awsimagebuildertypes.ContainerRecipeSummary{{Arn: aws.String(contARN)}},
		containerRecipes: map[string]awsimagebuildertypes.ContainerRecipe{contARN: {
			Arn:                    aws.String(contARN),
			Name:                   aws.String("api"),
			ContainerType:          awsimagebuildertypes.ContainerTypeDocker,
			KmsKeyId:               aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
			DockerfileTemplateData: aws.String("FROM amazonlinux\nRUN secret-build-step"),
			TargetRepository: &awsimagebuildertypes.TargetContainerRepository{
				RepositoryName: aws.String("app-images"),
				Service:        awsimagebuildertypes.ContainerRepositoryServiceEcr,
			},
		}},
		infraSummaries: []awsimagebuildertypes.InfrastructureConfigurationSummary{{Arn: aws.String(infraARN)}},
		infraConfigs: map[string]awsimagebuildertypes.InfrastructureConfiguration{infraARN: {
			Arn:                 aws.String(infraARN),
			Name:                aws.String("builders"),
			InstanceProfileName: aws.String("ImageBuilderInstanceProfile"),
			SubnetId:            aws.String("subnet-0abc123"),
			SecurityGroupIds:    []string{"sg-0aaa111"},
			SnsTopicArn:         aws.String("arn:aws:sns:us-east-1:123456789012:events"),
			KeyPair:             aws.String("builder-key"),
			Logging: &awsimagebuildertypes.Logging{
				S3Logs: &awsimagebuildertypes.S3Logs{
					S3BucketName: aws.String("imagebuilder-logs"),
					S3KeyPrefix:  aws.String("builds/"),
				},
			},
		}},
		distSummaries: []awsimagebuildertypes.DistributionConfigurationSummary{{Arn: aws.String(distARN)}},
		distConfigs: map[string]awsimagebuildertypes.DistributionConfiguration{distARN: {
			Arn:  aws.String(distARN),
			Name: aws.String("multi"),
			Distributions: []awsimagebuildertypes.Distribution{
				{Region: aws.String("us-east-1")},
				{Region: aws.String("us-west-2")},
			},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Pipelines) != 1 || snapshot.Pipelines[0].ImageRecipeARN != recipeARN {
		t.Fatalf("pipelines = %#v, want one with image recipe %q", snapshot.Pipelines, recipeARN)
	}
	if len(snapshot.ImageRecipes) != 1 || snapshot.ImageRecipes[0].ParentImage != "ami-0123456789abcdef0" {
		t.Fatalf("image recipes = %#v, want one with parent image", snapshot.ImageRecipes)
	}
	if len(snapshot.ImageRecipes[0].ComponentARNs) != 1 {
		t.Fatalf("image recipe component ARNs = %#v, want one", snapshot.ImageRecipes[0].ComponentARNs)
	}
	if len(snapshot.ContainerRecipes) != 1 {
		t.Fatalf("container recipes = %#v, want one", snapshot.ContainerRecipes)
	}
	recipe := snapshot.ContainerRecipes[0]
	if recipe.TargetRepositoryName != "app-images" || recipe.TargetRepositoryService != "ECR" {
		t.Fatalf("container recipe target = %q/%q, want app-images/ECR", recipe.TargetRepositoryName, recipe.TargetRepositoryService)
	}
	if len(snapshot.InfrastructureConfigurations) != 1 {
		t.Fatalf("infra configs = %#v, want one", snapshot.InfrastructureConfigurations)
	}
	infra := snapshot.InfrastructureConfigurations[0]
	if infra.InstanceProfileName != "ImageBuilderInstanceProfile" || infra.LoggingS3BucketName != "imagebuilder-logs" {
		t.Fatalf("infra mapping wrong: %#v", infra)
	}
	if !infra.KeyPairConfigured {
		t.Fatalf("KeyPairConfigured = false, want true (key pair name present)")
	}
	if len(snapshot.DistributionConfigurations) != 1 || len(snapshot.DistributionConfigurations[0].Regions) != 2 {
		t.Fatalf("distribution configs = %#v, want one with two regions", snapshot.DistributionConfigurations)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceImageBuilder,
	}
}

type fakeImageBuilderAPI struct {
	pipelines                []awsimagebuildertypes.ImagePipeline
	imageRecipeSummaries     []awsimagebuildertypes.ImageRecipeSummary
	imageRecipes             map[string]awsimagebuildertypes.ImageRecipe
	containerRecipeSummaries []awsimagebuildertypes.ContainerRecipeSummary
	containerRecipes         map[string]awsimagebuildertypes.ContainerRecipe
	infraSummaries           []awsimagebuildertypes.InfrastructureConfigurationSummary
	infraConfigs             map[string]awsimagebuildertypes.InfrastructureConfiguration
	distSummaries            []awsimagebuildertypes.DistributionConfigurationSummary
	distConfigs              map[string]awsimagebuildertypes.DistributionConfiguration
}

func (f *fakeImageBuilderAPI) ListImagePipelines(
	context.Context, *awsimagebuilder.ListImagePipelinesInput, ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.ListImagePipelinesOutput, error) {
	return &awsimagebuilder.ListImagePipelinesOutput{ImagePipelineList: f.pipelines}, nil
}

func (f *fakeImageBuilderAPI) ListImageRecipes(
	context.Context, *awsimagebuilder.ListImageRecipesInput, ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.ListImageRecipesOutput, error) {
	return &awsimagebuilder.ListImageRecipesOutput{ImageRecipeSummaryList: f.imageRecipeSummaries}, nil
}

func (f *fakeImageBuilderAPI) GetImageRecipe(
	_ context.Context, input *awsimagebuilder.GetImageRecipeInput, _ ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.GetImageRecipeOutput, error) {
	recipe := f.imageRecipes[aws.ToString(input.ImageRecipeArn)]
	return &awsimagebuilder.GetImageRecipeOutput{ImageRecipe: &recipe}, nil
}

func (f *fakeImageBuilderAPI) ListContainerRecipes(
	context.Context, *awsimagebuilder.ListContainerRecipesInput, ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.ListContainerRecipesOutput, error) {
	return &awsimagebuilder.ListContainerRecipesOutput{ContainerRecipeSummaryList: f.containerRecipeSummaries}, nil
}

func (f *fakeImageBuilderAPI) GetContainerRecipe(
	_ context.Context, input *awsimagebuilder.GetContainerRecipeInput, _ ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.GetContainerRecipeOutput, error) {
	recipe := f.containerRecipes[aws.ToString(input.ContainerRecipeArn)]
	return &awsimagebuilder.GetContainerRecipeOutput{ContainerRecipe: &recipe}, nil
}

func (f *fakeImageBuilderAPI) ListInfrastructureConfigurations(
	context.Context, *awsimagebuilder.ListInfrastructureConfigurationsInput, ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.ListInfrastructureConfigurationsOutput, error) {
	return &awsimagebuilder.ListInfrastructureConfigurationsOutput{
		InfrastructureConfigurationSummaryList: f.infraSummaries,
	}, nil
}

func (f *fakeImageBuilderAPI) GetInfrastructureConfiguration(
	_ context.Context, input *awsimagebuilder.GetInfrastructureConfigurationInput, _ ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.GetInfrastructureConfigurationOutput, error) {
	config := f.infraConfigs[aws.ToString(input.InfrastructureConfigurationArn)]
	return &awsimagebuilder.GetInfrastructureConfigurationOutput{InfrastructureConfiguration: &config}, nil
}

func (f *fakeImageBuilderAPI) ListDistributionConfigurations(
	context.Context, *awsimagebuilder.ListDistributionConfigurationsInput, ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.ListDistributionConfigurationsOutput, error) {
	return &awsimagebuilder.ListDistributionConfigurationsOutput{
		DistributionConfigurationSummaryList: f.distSummaries,
	}, nil
}

func (f *fakeImageBuilderAPI) GetDistributionConfiguration(
	_ context.Context, input *awsimagebuilder.GetDistributionConfigurationInput, _ ...func(*awsimagebuilder.Options),
) (*awsimagebuilder.GetDistributionConfigurationOutput, error) {
	config := f.distConfigs[aws.ToString(input.DistributionConfigurationArn)]
	return &awsimagebuilder.GetDistributionConfigurationOutput{DistributionConfiguration: &config}, nil
}
