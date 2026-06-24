// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapprunnertypes "github.com/aws/aws-sdk-go-v2/service/apprunner/types"

	apprunnerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apprunner"
)

// TestMapServiceDropsEnvironmentValuesAndKeepsSecretRefs proves the adapter
// never carries runtime environment-variable VALUES across the boundary: only
// names survive, and secret references survive as ARN-only references. It also
// locks the dangling-edge join key (resource identity is the service ARN) and
// confirms the source-config type, image, roles, KMS, and VPC connector map.
func TestMapServiceDropsEnvironmentValuesAndKeepsSecretRefs(t *testing.T) {
	service := mapService(&awsapprunnertypes.Service{
		ServiceArn:  aws.String("arn:aws:apprunner:us-east-1:123456789012:service/web/abc"),
		ServiceId:   aws.String("abc"),
		ServiceName: aws.String("web"),
		ServiceUrl:  aws.String("web.us-east-1.awsapprunner.com"),
		Status:      awsapprunnertypes.ServiceStatusRunning,
		SourceConfiguration: &awsapprunnertypes.SourceConfiguration{
			AuthenticationConfiguration: &awsapprunnertypes.AuthenticationConfiguration{
				AccessRoleArn: aws.String("arn:aws:iam::123456789012:role/ecr-access"),
			},
			ImageRepository: &awsapprunnertypes.ImageRepository{
				ImageIdentifier:     aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/web:prod"),
				ImageRepositoryType: awsapprunnertypes.ImageRepositoryTypeEcr,
				ImageConfiguration: &awsapprunnertypes.ImageConfiguration{
					RuntimeEnvironmentVariables: map[string]string{
						"APP_MODE":  "production-should-not-leak",
						"LOG_LEVEL": "debug-should-not-leak",
					},
					RuntimeEnvironmentSecrets: map[string]string{
						"DB_TOKEN": "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-token",
						"API_KEY":  "arn:aws:ssm:us-east-1:123456789012:parameter/app/key",
					},
				},
			},
		},
		InstanceConfiguration: &awsapprunnertypes.InstanceConfiguration{
			InstanceRoleArn: aws.String("arn:aws:iam::123456789012:role/instance"),
		},
		NetworkConfiguration: &awsapprunnertypes.NetworkConfiguration{
			EgressConfiguration: &awsapprunnertypes.EgressConfiguration{
				EgressType:      awsapprunnertypes.EgressTypeVpc,
				VpcConnectorArn: aws.String("arn:aws:apprunner:us-east-1:123456789012:vpcconnector/p/1/vc"),
			},
			IngressConfiguration: &awsapprunnertypes.IngressConfiguration{IsPubliclyAccessible: true},
		},
		EncryptionConfiguration: &awsapprunnertypes.EncryptionConfiguration{
			KmsKey: aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
		},
		AutoScalingConfigurationSummary: &awsapprunnertypes.AutoScalingConfigurationSummary{
			AutoScalingConfigurationArn: aws.String("arn:aws:apprunner:us-east-1:123456789012:autoscalingconfiguration/h/1/asc"),
		},
		ObservabilityConfiguration: &awsapprunnertypes.ServiceObservabilityConfiguration{
			ObservabilityEnabled:          true,
			ObservabilityConfigurationArn: aws.String("arn:aws:apprunner:us-east-1:123456789012:observabilityconfiguration/x/1/obs"),
		},
		HealthCheckConfiguration: &awsapprunnertypes.HealthCheckConfiguration{
			Protocol: awsapprunnertypes.HealthCheckProtocolHttp,
			Path:     aws.String("/health"),
			Interval: aws.Int32(5),
		},
	}, map[string]string{"team": "payments"})

	if service.ARN != "arn:aws:apprunner:us-east-1:123456789012:service/web/abc" {
		t.Fatalf("service ARN = %q, want the service ARN join key", service.ARN)
	}
	if service.SourceConfigurationType != "IMAGE" {
		t.Fatalf("source configuration type = %q, want IMAGE", service.SourceConfigurationType)
	}
	if service.ImageIdentifier != "123456789012.dkr.ecr.us-east-1.amazonaws.com/web:prod" {
		t.Fatalf("image identifier = %q", service.ImageIdentifier)
	}
	if service.AccessRoleARN != "arn:aws:iam::123456789012:role/ecr-access" {
		t.Fatalf("access role ARN = %q", service.AccessRoleARN)
	}
	if service.InstanceRoleARN != "arn:aws:iam::123456789012:role/instance" {
		t.Fatalf("instance role ARN = %q", service.InstanceRoleARN)
	}
	if service.KMSKey != "arn:aws:kms:us-east-1:123456789012:key/abcd" {
		t.Fatalf("kms key = %q", service.KMSKey)
	}
	if service.VpcConnectorARN == "" {
		t.Fatalf("vpc connector ARN not mapped")
	}
	if len(service.EnvironmentVariableNames) != 2 || service.EnvironmentVariableNames[0] != "APP_MODE" {
		t.Fatalf("environment variable names = %v, want sorted [APP_MODE LOG_LEVEL]", service.EnvironmentVariableNames)
	}
	if len(service.SecretReferences) != 2 {
		t.Fatalf("secret references = %d, want 2", len(service.SecretReferences))
	}
	for _, secret := range service.SecretReferences {
		if secret.ValueFrom == "" {
			t.Fatalf("secret %q ValueFrom missing", secret.Name)
		}
	}
	// The scanner-owned Service type has no field that could carry an
	// environment-variable value, so a leak is structurally impossible.
	for _, field := range []string{"EnvironmentVariables", "RuntimeEnvironmentVariables", "EnvironmentValues"} {
		if _, ok := reflect.TypeOf(apprunnerservice.Service{}).FieldByName(field); ok {
			t.Fatalf("Service type must not declare a %q field", field)
		}
	}
}

func TestMapVpcConnectorCarriesSubnetsAndSecurityGroups(t *testing.T) {
	connector := mapVpcConnector(awsapprunnertypes.VpcConnector{
		VpcConnectorArn:      aws.String("arn:aws:apprunner:us-east-1:123456789012:vpcconnector/p/1/vc"),
		VpcConnectorName:     aws.String("private"),
		VpcConnectorRevision: 1,
		Status:               awsapprunnertypes.VpcConnectorStatusActive,
		Subnets:              []string{"subnet-aaa", "subnet-bbb"},
		SecurityGroups:       []string{"sg-111"},
	})
	if len(connector.Subnets) != 2 {
		t.Fatalf("subnet count = %d, want 2", len(connector.Subnets))
	}
	if len(connector.SecurityGroups) != 1 {
		t.Fatalf("security group count = %d, want 1", len(connector.SecurityGroups))
	}
}

func TestMapVpcIngressConnectionCarriesServiceJoin(t *testing.T) {
	ingress := mapVpcIngressConnection(&awsapprunnertypes.VpcIngressConnection{
		VpcIngressConnectionArn:  aws.String("arn:aws:apprunner:us-east-1:123456789012:vpcingressconnection/i/vic"),
		VpcIngressConnectionName: aws.String("ing"),
		Status:                   awsapprunnertypes.VpcIngressConnectionStatusAvailable,
		DomainName:               aws.String("internal.example.com"),
		ServiceArn:               aws.String("arn:aws:apprunner:us-east-1:123456789012:service/web/abc"),
		IngressVpcConfiguration: &awsapprunnertypes.IngressVpcConfiguration{
			VpcEndpointId: aws.String("vpce-123"),
			VpcId:         aws.String("vpc-abc"),
		},
	})
	if ingress.ServiceARN != "arn:aws:apprunner:us-east-1:123456789012:service/web/abc" {
		t.Fatalf("service ARN join = %q", ingress.ServiceARN)
	}
	if ingress.VpcEndpointID != "vpce-123" || ingress.VpcID != "vpc-abc" {
		t.Fatalf("ingress VPC config = %q / %q", ingress.VpcEndpointID, ingress.VpcID)
	}
}

func TestMapCodeRepositoryServiceUsesConnection(t *testing.T) {
	service := mapService(&awsapprunnertypes.Service{
		ServiceArn:  aws.String("arn:aws:apprunner:us-east-1:123456789012:service/api/def"),
		ServiceName: aws.String("api"),
		Status:      awsapprunnertypes.ServiceStatusRunning,
		SourceConfiguration: &awsapprunnertypes.SourceConfiguration{
			AuthenticationConfiguration: &awsapprunnertypes.AuthenticationConfiguration{
				ConnectionArn: aws.String("arn:aws:apprunner:us-east-1:123456789012:connection/github/c"),
			},
			CodeRepository: &awsapprunnertypes.CodeRepository{
				RepositoryUrl: aws.String("https://github.com/example/api"),
			},
		},
	}, nil)
	if service.SourceConfigurationType != "REPOSITORY" {
		t.Fatalf("source configuration type = %q, want REPOSITORY", service.SourceConfigurationType)
	}
	if service.ConnectionARN != "arn:aws:apprunner:us-east-1:123456789012:connection/github/c" {
		t.Fatalf("connection ARN = %q", service.ConnectionARN)
	}
	if service.CodeRepositoryURL != "https://github.com/example/api" {
		t.Fatalf("code repository URL = %q", service.CodeRepositoryURL)
	}
}
