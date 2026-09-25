// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"
	awssagemakertypes "github.com/aws/aws-sdk-go-v2/service/sagemaker/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListModelsMapsImageArtifactRoleAndDropsEnvironment(t *testing.T) {
	api := &fakeSageMakerAPI{
		models: []awssagemakertypes.ModelSummary{{
			ModelArn:  awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:model/m"),
			ModelName: awsv2.String("m"),
		}},
		describeModel: &awssagemaker.DescribeModelOutput{
			ModelArn:         awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:model/m"),
			ModelName:        awsv2.String("m"),
			ExecutionRoleArn: awsv2.String("arn:aws:iam::123456789012:role/model"),
			PrimaryContainer: &awssagemakertypes.ContainerDefinition{
				Image:        awsv2.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/infer:latest"),
				ModelDataUrl: awsv2.String("s3://artifacts/model.tar.gz"),
				Environment:  map[string]string{"DB_PASSWORD": "container-env-secret"},
			},
			VpcConfig: &awssagemakertypes.VpcConfig{Subnets: []string{"subnet-aaa"}},
		},
	}
	adapter := newTestClient(api)

	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v, want nil", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	model := models[0]
	if model.ExecutionRole != "arn:aws:iam::123456789012:role/model" {
		t.Fatalf("ExecutionRole = %q", model.ExecutionRole)
	}
	if len(model.Containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1", len(model.Containers))
	}
	container := model.Containers[0]
	if container.Image != "123456789012.dkr.ecr.us-east-1.amazonaws.com/infer:latest" {
		t.Fatalf("Image = %q", container.Image)
	}
	if container.ModelDataURL != "s3://artifacts/model.tar.gz" {
		t.Fatalf("ModelDataURL = %q", container.ModelDataURL)
	}
	// The scanner-owned ModelContainer carries only Image and ModelDataURL; the
	// SDK environment secret has no field to land in and must not appear in
	// either captured field.
	if contains(container.Image, "container-env-secret") || contains(container.ModelDataURL, "container-env-secret") {
		t.Fatalf("model container leaked environment secret: image=%q url=%q", container.Image, container.ModelDataURL)
	}
}

func TestClientListTrainingJobsReadsRoleNotHyperParameters(t *testing.T) {
	api := &fakeSageMakerAPI{
		trainingJobs: []awssagemakertypes.TrainingJobSummary{{
			TrainingJobArn:    awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:training-job/tj"),
			TrainingJobName:   awsv2.String("tj"),
			TrainingJobStatus: awssagemakertypes.TrainingJobStatusCompleted,
		}},
		describeTrainingJob: &awssagemaker.DescribeTrainingJobOutput{
			TrainingJobArn:  awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:training-job/tj"),
			TrainingJobName: awsv2.String("tj"),
			RoleArn:         awsv2.String("arn:aws:iam::123456789012:role/train"),
			HyperParameters: map[string]string{"learning_rate": "secret-hyperparameter"},
		},
	}
	adapter := newTestClient(api)

	jobs, err := adapter.ListTrainingJobs(context.Background())
	if err != nil {
		t.Fatalf("ListTrainingJobs() error = %v, want nil", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].ExecutionRole != "arn:aws:iam::123456789012:role/train" {
		t.Fatalf("ExecutionRole = %q, want training role", jobs[0].ExecutionRole)
	}
	// The scanner-owned TrainingJob type has no hyperparameter field at all, so
	// the secret value cannot be carried forward.
}

func TestClientListEndpointsAndConfigsResolveModelDependency(t *testing.T) {
	api := &fakeSageMakerAPI{
		endpoints: []awssagemakertypes.EndpointSummary{{
			EndpointArn:    awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:endpoint/e"),
			EndpointName:   awsv2.String("e"),
			EndpointStatus: awssagemakertypes.EndpointStatusInService,
		}},
		describeEndpoint: &awssagemaker.DescribeEndpointOutput{
			EndpointName:       awsv2.String("e"),
			EndpointConfigName: awsv2.String("ec"),
		},
		endpointConfigs: []awssagemakertypes.EndpointConfigSummary{{
			EndpointConfigArn:  awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:endpoint-config/ec"),
			EndpointConfigName: awsv2.String("ec"),
		}},
		describeEndpointConfig: &awssagemaker.DescribeEndpointConfigOutput{
			EndpointConfigName: awsv2.String("ec"),
			ProductionVariants: []awssagemakertypes.ProductionVariant{{
				VariantName: awsv2.String("v1"),
				ModelName:   awsv2.String("m"),
			}},
		},
	}
	adapter := newTestClient(api)

	endpoints, err := adapter.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v, want nil", err)
	}
	if endpoints[0].EndpointConfig != "ec" {
		t.Fatalf("EndpointConfig = %q, want ec", endpoints[0].EndpointConfig)
	}
	configs, err := adapter.ListEndpointConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListEndpointConfigs() error = %v, want nil", err)
	}
	if len(configs[0].ModelNames) != 1 || configs[0].ModelNames[0] != "m" {
		t.Fatalf("ModelNames = %#v, want [m]", configs[0].ModelNames)
	}
}

func TestClientListNotebookInstancesReadsSubnetNotScriptBody(t *testing.T) {
	api := &fakeSageMakerAPI{
		notebooks: []awssagemakertypes.NotebookInstanceSummary{{
			NotebookInstanceArn:    awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:notebook-instance/nb"),
			NotebookInstanceName:   awsv2.String("nb"),
			NotebookInstanceStatus: awssagemakertypes.NotebookInstanceStatusInService,
		}},
		describeNotebook: &awssagemaker.DescribeNotebookInstanceOutput{
			NotebookInstanceName:                awsv2.String("nb"),
			SubnetId:                            awsv2.String("subnet-aaa"),
			SecurityGroups:                      []string{"sg-1"},
			DirectInternetAccess:                awssagemakertypes.DirectInternetAccessDisabled,
			NotebookInstanceLifecycleConfigName: awsv2.String("nb-lifecycle"),
		},
	}
	adapter := newTestClient(api)

	notebooks, err := adapter.ListNotebookInstances(context.Background())
	if err != nil {
		t.Fatalf("ListNotebookInstances() error = %v, want nil", err)
	}
	notebook := notebooks[0]
	if notebook.SubnetID != "subnet-aaa" {
		t.Fatalf("SubnetID = %q, want subnet-aaa", notebook.SubnetID)
	}
	if notebook.LifecycleConfigName != "nb-lifecycle" {
		t.Fatalf("LifecycleConfigName = %q, want nb-lifecycle", notebook.LifecycleConfigName)
	}
	// The scanner-owned NotebookInstance has no lifecycle script body field;
	// the adapter never calls DescribeNotebookInstanceLifecycleConfig.
	if api.calledOps["DescribeNotebookInstanceLifecycleConfig"] {
		t.Fatalf("adapter called DescribeNotebookInstanceLifecycleConfig; script bodies must stay unread")
	}
}

func TestClientListDomainsReadsVPC(t *testing.T) {
	api := &fakeSageMakerAPI{
		domains: []awssagemakertypes.DomainDetails{{
			DomainArn:  awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:domain/d-1"),
			DomainId:   awsv2.String("d-1"),
			DomainName: awsv2.String("studio"),
			Status:     awssagemakertypes.DomainStatusInService,
		}},
		describeDomain: &awssagemaker.DescribeDomainOutput{
			DomainId:  awsv2.String("d-1"),
			VpcId:     awsv2.String("vpc-aaa"),
			AuthMode:  awssagemakertypes.AuthModeIam,
			SubnetIds: []string{"subnet-aaa"},
		},
	}
	adapter := newTestClient(api)

	domains, err := adapter.ListDomains(context.Background())
	if err != nil {
		t.Fatalf("ListDomains() error = %v, want nil", err)
	}
	if domains[0].VPCID != "vpc-aaa" {
		t.Fatalf("VPCID = %q, want vpc-aaa", domains[0].VPCID)
	}
}

func TestClientListPipelinesNeverReadsDefinitionBody(t *testing.T) {
	api := &fakeSageMakerAPI{
		pipelines: []awssagemakertypes.PipelineSummary{{
			PipelineArn:  awsv2.String("arn:aws:sagemaker:us-east-1:123456789012:pipeline/pl"),
			PipelineName: awsv2.String("pl"),
		}},
	}
	adapter := newTestClient(api)

	if _, err := adapter.ListPipelines(context.Background()); err != nil {
		t.Fatalf("ListPipelines() error = %v, want nil", err)
	}
	if api.calledOps["DescribePipeline"] || api.calledOps["DescribePipelineDefinitionForExecution"] {
		t.Fatalf("adapter called a pipeline-definition read; definition bodies must stay unread")
	}
}

func newTestClient(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceSageMaker},
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
