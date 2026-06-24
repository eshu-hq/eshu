// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apprunner_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apprunner"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	serviceARN       = "arn:aws:apprunner:us-east-1:123456789012:service/web/abc123"
	imageServiceARN  = "arn:aws:apprunner:us-east-1:123456789012:service/api/def456"
	connectionARN    = "arn:aws:apprunner:us-east-1:123456789012:connection/github-main/c1"
	autoScalingARN   = "arn:aws:apprunner:us-east-1:123456789012:autoscalingconfiguration/high/1/asc1"
	observabilityARN = "arn:aws:apprunner:us-east-1:123456789012:observabilityconfiguration/xray/1/obs1"
	vpcConnectorARN  = "arn:aws:apprunner:us-east-1:123456789012:vpcconnector/private/1/vc1"
	vpcIngressARN    = "arn:aws:apprunner:us-east-1:123456789012:vpcingressconnection/ing/vic1"
	accessRoleARN    = "arn:aws:iam::123456789012:role/apprunner-ecr-access"
	instanceRoleARN  = "arn:aws:iam::123456789012:role/apprunner-instance"
	kmsKeyARN        = "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	imageURI         = "123456789012.dkr.ecr.us-east-1.amazonaws.com/web:prod"
	secretARN        = "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-token"
	ssmSecretARN     = "arn:aws:ssm:us-east-1:123456789012:parameter/app/key"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAppRunner,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
	}
}

// fakeClient is a metadata-only App Runner read surface for scanner tests.
type fakeClient struct {
	services                    []apprunner.Service
	connections                 []apprunner.Connection
	autoScalingConfigurations   []apprunner.AutoScalingConfiguration
	observabilityConfigurations []apprunner.ObservabilityConfiguration
	vpcConnectors               []apprunner.VpcConnector
	vpcIngressConnections       []apprunner.VpcIngressConnection
	err                         error
}

func (c fakeClient) ListServices(context.Context) ([]apprunner.Service, error) {
	return c.services, c.err
}

func (c fakeClient) ListConnections(context.Context) ([]apprunner.Connection, error) {
	return c.connections, c.err
}

func (c fakeClient) ListAutoScalingConfigurations(context.Context) ([]apprunner.AutoScalingConfiguration, error) {
	return c.autoScalingConfigurations, c.err
}

func (c fakeClient) ListObservabilityConfigurations(context.Context) ([]apprunner.ObservabilityConfiguration, error) {
	return c.observabilityConfigurations, c.err
}

func (c fakeClient) ListVpcConnectors(context.Context) ([]apprunner.VpcConnector, error) {
	return c.vpcConnectors, c.err
}

func (c fakeClient) ListVpcIngressConnections(context.Context) ([]apprunner.VpcIngressConnection, error) {
	return c.vpcIngressConnections, c.err
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType {
			out = append(out, envelope.Payload)
		}
	}
	return out
}

func sampleClient() fakeClient {
	return fakeClient{
		services: []apprunner.Service{
			{
				ARN:                           serviceARN,
				ID:                            "abc123",
				Name:                          "web",
				Status:                        "RUNNING",
				ServiceURL:                    "web.us-east-1.awsapprunner.com",
				SourceConfigurationType:       "IMAGE",
				ImageIdentifier:               imageURI,
				ImageRepositoryType:           "ECR",
				AutoDeploymentsEnabled:        true,
				AccessRoleARN:                 accessRoleARN,
				InstanceRoleARN:               instanceRoleARN,
				KMSKey:                        kmsKeyARN,
				VpcConnectorARN:               vpcConnectorARN,
				EgressType:                    "VPC",
				IsPubliclyAccessible:          true,
				AutoScalingConfigurationARN:   autoScalingARN,
				ObservabilityEnabled:          true,
				ObservabilityConfigurationARN: observabilityARN,
				HealthCheck: apprunner.HealthCheck{
					Protocol:           "HTTP",
					Path:               "/health",
					Interval:           5,
					Timeout:            2,
					HealthyThreshold:   1,
					UnhealthyThreshold: 5,
				},
				EnvironmentVariableNames: []string{"APP_MODE", "LOG_LEVEL"},
				SecretReferences: []apprunner.SecretReference{
					{Name: "DB_TOKEN", ValueFrom: secretARN},
					{Name: "API_KEY", ValueFrom: ssmSecretARN},
				},
				Tags: map[string]string{"team": "payments"},
			},
			{
				ARN:                         imageServiceARN,
				ID:                          "def456",
				Name:                        "api",
				Status:                      "RUNNING",
				SourceConfigurationType:     "REPOSITORY",
				CodeRepositoryURL:           "https://github.com/example/api",
				ConnectionARN:               connectionARN,
				InstanceRoleARN:             instanceRoleARN,
				AutoScalingConfigurationARN: autoScalingARN,
			},
		},
		connections: []apprunner.Connection{{
			ARN:          connectionARN,
			Name:         "github-main",
			ProviderType: "GITHUB",
			Status:       "AVAILABLE",
		}},
		autoScalingConfigurations: []apprunner.AutoScalingConfiguration{{
			ARN:            autoScalingARN,
			Name:           "high",
			Revision:       1,
			Status:         "ACTIVE",
			IsDefault:      false,
			MaxConcurrency: 100,
			MaxSize:        10,
			MinSize:        1,
		}},
		observabilityConfigurations: []apprunner.ObservabilityConfiguration{{
			ARN:         observabilityARN,
			Name:        "xray",
			Revision:    1,
			Status:      "ACTIVE",
			TraceVendor: "AWSXRAY",
		}},
		vpcConnectors: []apprunner.VpcConnector{{
			ARN:            vpcConnectorARN,
			Name:           "private",
			Revision:       1,
			Status:         "ACTIVE",
			Subnets:        []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroups: []string{"sg-111"},
		}},
		vpcIngressConnections: []apprunner.VpcIngressConnection{{
			ARN:           vpcIngressARN,
			Name:          "ing",
			Status:        "AVAILABLE",
			DomainName:    "internal.example.com",
			ServiceARN:    serviceARN,
			VpcEndpointID: "vpce-123",
			VpcID:         "vpc-abc",
		}},
	}
}
