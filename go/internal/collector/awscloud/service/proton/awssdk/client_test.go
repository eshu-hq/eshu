// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsproton "github.com/aws/aws-sdk-go-v2/service/proton"
	awsprotontypes "github.com/aws/aws-sdk-go-v2/service/proton/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsProtonMetadataOnly(t *testing.T) {
	envARN := "arn:aws:proton:us-east-1:123456789012:environment/prod"
	serviceARN := "arn:aws:proton:us-east-1:123456789012:service/orders"
	roleARN := "arn:aws:iam::123456789012:role/proton-service-role"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeProtonAPI{
		environments: []awsprotontypes.EnvironmentSummary{{
			Arn:                  aws.String(envARN),
			Name:                 aws.String("prod"),
			TemplateName:         aws.String("fargate-env"),
			Provisioning:         awsprotontypes.ProvisioningCustomerManaged,
			DeploymentStatus:     awsprotontypes.DeploymentStatusSucceeded,
			ProtonServiceRoleArn: aws.String(roleARN),
			CreatedAt:            aws.Time(createdAt),
		}},
		services: []awsprotontypes.ServiceSummary{{
			Arn:          aws.String(serviceARN),
			Name:         aws.String("orders"),
			TemplateName: aws.String("lb-web"),
			Status:       awsprotontypes.ServiceStatusActive,
			CreatedAt:    aws.Time(createdAt),
		}},
		serviceDetail: map[string]*awsprotontypes.Service{
			"orders": {
				Arn:          aws.String(serviceARN),
				Name:         aws.String("orders"),
				TemplateName: aws.String("lb-web"),
				BranchName:   aws.String("main"),
				RepositoryId: aws.String("acme/orders"),
				// Spec body is present on the SDK detail but must never be mapped.
				Spec: aws.String("---\nproton: ServiceSpec\npipeline:\n  secret: do-not-persist\n"),
			},
		},
		environmentTemplates: []awsprotontypes.EnvironmentTemplateSummary{{
			Arn:                aws.String("arn:aws:proton:us-east-1:123456789012:environment-template/fargate-env"),
			Name:               aws.String("fargate-env"),
			DisplayName:        aws.String("Fargate Environment"),
			Provisioning:       awsprotontypes.ProvisioningCustomerManaged,
			RecommendedVersion: aws.String("1.0"),
		}},
		serviceTemplates: []awsprotontypes.ServiceTemplateSummary{{
			Arn:                  aws.String("arn:aws:proton:us-east-1:123456789012:service-template/lb-web"),
			Name:                 aws.String("lb-web"),
			DisplayName:          aws.String("Load Balanced Web"),
			PipelineProvisioning: awsprotontypes.ProvisioningCustomerManaged,
			RecommendedVersion:   aws.String("2.1"),
		}},
		serviceInstances: []awsprotontypes.ServiceInstanceSummary{{
			Name:            aws.String("orders-1"),
			ServiceName:     aws.String("orders"),
			EnvironmentName: aws.String("prod"),
		}},
		tags: map[string][]awsprotontypes.Tag{
			envARN:     {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			serviceARN: {{Key: aws.String("Team"), Value: aws.String("checkout")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Environments) != 1 {
		t.Fatalf("len(Environments) = %d, want 1", len(snapshot.Environments))
	}
	environment := snapshot.Environments[0]
	if environment.ARN != envARN {
		t.Fatalf("environment ARN = %q, want %q", environment.ARN, envARN)
	}
	if environment.ProtonServiceRoleArn != roleARN {
		t.Fatalf("environment ProtonServiceRoleArn = %q, want %q", environment.ProtonServiceRoleArn, roleARN)
	}
	if environment.Provisioning != "CUSTOMER_MANAGED" {
		t.Fatalf("environment Provisioning = %q, want CUSTOMER_MANAGED", environment.Provisioning)
	}
	if environment.Tags["Environment"] != "prod" {
		t.Fatalf("environment tag Environment = %q, want prod", environment.Tags["Environment"])
	}

	if len(snapshot.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(snapshot.Services))
	}
	service := snapshot.Services[0]
	if service.RepositoryID != "acme/orders" {
		t.Fatalf("service RepositoryID = %q, want acme/orders", service.RepositoryID)
	}
	if service.BranchName != "main" {
		t.Fatalf("service BranchName = %q, want main", service.BranchName)
	}
	if service.Status != "ACTIVE" {
		t.Fatalf("service Status = %q, want ACTIVE", service.Status)
	}

	if len(snapshot.EnvironmentTemplates) != 1 || snapshot.EnvironmentTemplates[0].Name != "fargate-env" {
		t.Fatalf("EnvironmentTemplates = %#v, want one fargate-env", snapshot.EnvironmentTemplates)
	}
	if len(snapshot.ServiceTemplates) != 1 || snapshot.ServiceTemplates[0].Provisioning != "CUSTOMER_MANAGED" {
		t.Fatalf("ServiceTemplates = %#v, want one CUSTOMER_MANAGED template", snapshot.ServiceTemplates)
	}

	if len(snapshot.ServicePlacements) != 1 {
		t.Fatalf("len(ServicePlacements) = %d, want 1", len(snapshot.ServicePlacements))
	}
	placement := snapshot.ServicePlacements[0]
	if placement.ServiceName != "orders" || placement.EnvironmentName != "prod" {
		t.Fatalf("placement = %#v, want orders->prod", placement)
	}
}

func TestClientPaginatesEnvironments(t *testing.T) {
	api := &fakeProtonAPI{
		environmentPages: [][]awsprotontypes.EnvironmentSummary{
			{{Arn: aws.String("arn:aws:proton:us-east-1:123456789012:environment/a"), Name: aws.String("a")}},
			{{Arn: aws.String("arn:aws:proton:us-east-1:123456789012:environment/b"), Name: aws.String("b")}},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Environments) != 2 {
		t.Fatalf("len(Environments) = %d, want 2 (pagination)", len(snapshot.Environments))
	}
}

type fakeProtonAPI struct {
	environments         []awsprotontypes.EnvironmentSummary
	environmentPages     [][]awsprotontypes.EnvironmentSummary
	environmentCall      int
	services             []awsprotontypes.ServiceSummary
	serviceDetail        map[string]*awsprotontypes.Service
	environmentTemplates []awsprotontypes.EnvironmentTemplateSummary
	serviceTemplates     []awsprotontypes.ServiceTemplateSummary
	serviceInstances     []awsprotontypes.ServiceInstanceSummary
	tags                 map[string][]awsprotontypes.Tag
}

func (f *fakeProtonAPI) ListEnvironments(
	_ context.Context,
	input *awsproton.ListEnvironmentsInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListEnvironmentsOutput, error) {
	if len(f.environmentPages) > 0 {
		if f.environmentCall >= len(f.environmentPages) {
			return &awsproton.ListEnvironmentsOutput{}, nil
		}
		page := f.environmentPages[f.environmentCall]
		f.environmentCall++
		var next *string
		if f.environmentCall < len(f.environmentPages) {
			next = aws.String("more")
		}
		return &awsproton.ListEnvironmentsOutput{Environments: page, NextToken: next}, nil
	}
	return &awsproton.ListEnvironmentsOutput{Environments: f.environments}, nil
}

func (f *fakeProtonAPI) ListServices(
	_ context.Context,
	_ *awsproton.ListServicesInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListServicesOutput, error) {
	return &awsproton.ListServicesOutput{Services: f.services}, nil
}

func (f *fakeProtonAPI) GetService(
	_ context.Context,
	input *awsproton.GetServiceInput,
	_ ...func(*awsproton.Options),
) (*awsproton.GetServiceOutput, error) {
	return &awsproton.GetServiceOutput{Service: f.serviceDetail[aws.ToString(input.Name)]}, nil
}

func (f *fakeProtonAPI) ListEnvironmentTemplates(
	_ context.Context,
	_ *awsproton.ListEnvironmentTemplatesInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListEnvironmentTemplatesOutput, error) {
	return &awsproton.ListEnvironmentTemplatesOutput{Templates: f.environmentTemplates}, nil
}

func (f *fakeProtonAPI) ListServiceTemplates(
	_ context.Context,
	_ *awsproton.ListServiceTemplatesInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListServiceTemplatesOutput, error) {
	return &awsproton.ListServiceTemplatesOutput{Templates: f.serviceTemplates}, nil
}

func (f *fakeProtonAPI) ListServiceInstances(
	_ context.Context,
	_ *awsproton.ListServiceInstancesInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListServiceInstancesOutput, error) {
	return &awsproton.ListServiceInstancesOutput{ServiceInstances: f.serviceInstances}, nil
}

func (f *fakeProtonAPI) ListTagsForResource(
	_ context.Context,
	input *awsproton.ListTagsForResourceInput,
	_ ...func(*awsproton.Options),
) (*awsproton.ListTagsForResourceOutput, error) {
	return &awsproton.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(input.ResourceArn)]}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceProton,
	}
}
