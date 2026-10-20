// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"
	awssagemakertypes "github.com/aws/aws-sdk-go-v2/service/sagemaker/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListModelsMapsImageArtifactRoleAndDropsEnvironment(t *testing.T) {
	api := &fakeSageMakerAPI{
		models: []awssagemakertypes.ModelSummary{{
			ModelArn:  aws.String("arn:aws:sagemaker:us-east-1:123456789012:model/m"),
			ModelName: aws.String("m"),
		}},
		describeModel: &awssagemaker.DescribeModelOutput{
			ModelArn:         aws.String("arn:aws:sagemaker:us-east-1:123456789012:model/m"),
			ModelName:        aws.String("m"),
			ExecutionRoleArn: aws.String("arn:aws:iam::123456789012:role/model"),
			PrimaryContainer: &awssagemakertypes.ContainerDefinition{
				Image:        aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/infer:latest"),
				ModelDataUrl: aws.String("s3://artifacts/model.tar.gz"),
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
			TrainingJobArn:    aws.String("arn:aws:sagemaker:us-east-1:123456789012:training-job/tj"),
			TrainingJobName:   aws.String("tj"),
			TrainingJobStatus: awssagemakertypes.TrainingJobStatusCompleted,
		}},
		describeTrainingJob: &awssagemaker.DescribeTrainingJobOutput{
			TrainingJobArn:  aws.String("arn:aws:sagemaker:us-east-1:123456789012:training-job/tj"),
			TrainingJobName: aws.String("tj"),
			RoleArn:         aws.String("arn:aws:iam::123456789012:role/train"),
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
			EndpointArn:    aws.String("arn:aws:sagemaker:us-east-1:123456789012:endpoint/e"),
			EndpointName:   aws.String("e"),
			EndpointStatus: awssagemakertypes.EndpointStatusInService,
		}},
		describeEndpoint: &awssagemaker.DescribeEndpointOutput{
			EndpointName:       aws.String("e"),
			EndpointConfigName: aws.String("ec"),
		},
		endpointConfigs: []awssagemakertypes.EndpointConfigSummary{{
			EndpointConfigArn:  aws.String("arn:aws:sagemaker:us-east-1:123456789012:endpoint-config/ec"),
			EndpointConfigName: aws.String("ec"),
		}},
		describeEndpointConfig: &awssagemaker.DescribeEndpointConfigOutput{
			EndpointConfigName: aws.String("ec"),
			ProductionVariants: []awssagemakertypes.ProductionVariant{{
				VariantName: aws.String("v1"),
				ModelName:   aws.String("m"),
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
			NotebookInstanceArn:    aws.String("arn:aws:sagemaker:us-east-1:123456789012:notebook-instance/nb"),
			NotebookInstanceName:   aws.String("nb"),
			NotebookInstanceStatus: awssagemakertypes.NotebookInstanceStatusInService,
		}},
		describeNotebook: &awssagemaker.DescribeNotebookInstanceOutput{
			NotebookInstanceName:                aws.String("nb"),
			SubnetId:                            aws.String("subnet-aaa"),
			SecurityGroups:                      []string{"sg-1"},
			DirectInternetAccess:                awssagemakertypes.DirectInternetAccessDisabled,
			NotebookInstanceLifecycleConfigName: aws.String("nb-lifecycle"),
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
			DomainArn:  aws.String("arn:aws:sagemaker:us-east-1:123456789012:domain/d-1"),
			DomainId:   aws.String("d-1"),
			DomainName: aws.String("studio"),
			Status:     awssagemakertypes.DomainStatusInService,
		}},
		describeDomain: &awssagemaker.DescribeDomainOutput{
			DomainId:  aws.String("d-1"),
			VpcId:     aws.String("vpc-aaa"),
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
			PipelineArn:  aws.String("arn:aws:sagemaker:us-east-1:123456789012:pipeline/pl"),
			PipelineName: aws.String("pl"),
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
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSageMaker},
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
