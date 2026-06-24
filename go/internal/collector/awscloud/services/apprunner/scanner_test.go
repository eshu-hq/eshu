// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apprunner_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apprunner"
)

func TestScannerRequiresClient(t *testing.T) {
	scanner := apprunner.Scanner{}
	if _, err := scanner.Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerRejectsForeignServiceKind(t *testing.T) {
	scanner := apprunner.Scanner{Client: fakeClient{}}
	boundary := testBoundary()
	boundary.ServiceKind = "ecs"
	if _, err := scanner.Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerSurfacesClientError(t *testing.T) {
	wrapped := errors.New("boom")
	scanner := apprunner.Scanner{Client: fakeClient{err: wrapped}}
	_, err := scanner.Scan(context.Background(), testBoundary())
	if !errors.Is(err, wrapped) {
		t.Fatalf("Scan() error = %v, want wrapped client error", err)
	}
}

func TestScannerEmitsAllResourceKinds(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, tc := range []struct {
		resourceType string
		want         int
	}{
		{awscloud.ResourceTypeAppRunnerService, 2},
		{awscloud.ResourceTypeAppRunnerConnection, 1},
		{awscloud.ResourceTypeAppRunnerAutoScalingConfiguration, 1},
		{awscloud.ResourceTypeAppRunnerObservabilityConfiguration, 1},
		{awscloud.ResourceTypeAppRunnerVpcConnector, 1},
		{awscloud.ResourceTypeAppRunnerVpcIngressConnection, 1},
	} {
		if got := resourcesByType(t, envelopes, tc.resourceType); len(got) != tc.want {
			t.Fatalf("resource %q count = %d, want %d", tc.resourceType, len(got), tc.want)
		}
	}
}

// TestServiceResourceIDIsServiceARN locks the dangling-edge join key: the App
// Runner service resource_id must be the service ARN so the ACM and WAFv2
// scanner edges that target aws_apprunner_service by service ARN resolve.
func TestServiceResourceIDIsServiceARN(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	services := resourcesByType(t, envelopes, awscloud.ResourceTypeAppRunnerService)
	var found bool
	for _, service := range services {
		if service["resource_id"] == serviceARN {
			found = true
			if service["arn"] != serviceARN {
				t.Fatalf("service arn = %v, want %q", service["arn"], serviceARN)
			}
		}
	}
	if !found {
		t.Fatalf("no App Runner service resource_id equals the service ARN %q", serviceARN)
	}
}

func TestServiceRelationshipsHaveTargetTypeAndJoinKeys(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	cases := []struct {
		relationshipType string
		targetType       string
		sourceResourceID string
		targetResourceID string
	}{
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesImage,
			targetType:       "container_image",
			sourceResourceID: serviceARN,
			targetResourceID: imageURI,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesConnection,
			targetType:       awscloud.ResourceTypeAppRunnerConnection,
			sourceResourceID: imageServiceARN,
			targetResourceID: connectionARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesIAMRole,
			targetType:       awscloud.ResourceTypeIAMRole,
			sourceResourceID: serviceARN,
			targetResourceID: accessRoleARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesIAMRole,
			targetType:       awscloud.ResourceTypeIAMRole,
			sourceResourceID: serviceARN,
			targetResourceID: instanceRoleARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesKMSKey,
			targetType:       awscloud.ResourceTypeKMSKey,
			sourceResourceID: serviceARN,
			targetResourceID: kmsKeyARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesVpcConnector,
			targetType:       awscloud.ResourceTypeAppRunnerVpcConnector,
			sourceResourceID: serviceARN,
			targetResourceID: vpcConnectorARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesAutoScalingConfiguration,
			targetType:       awscloud.ResourceTypeAppRunnerAutoScalingConfiguration,
			sourceResourceID: serviceARN,
			targetResourceID: autoScalingARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceUsesObservabilityConfiguration,
			targetType:       awscloud.ResourceTypeAppRunnerObservabilityConfiguration,
			sourceResourceID: serviceARN,
			targetResourceID: observabilityARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceReferencesSecret,
			targetType:       awscloud.ResourceTypeSecretsManagerSecret,
			sourceResourceID: serviceARN,
			targetResourceID: secretARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerServiceReferencesSecret,
			targetType:       awscloud.ResourceTypeSSMParameter,
			sourceResourceID: serviceARN,
			targetResourceID: ssmSecretARN,
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerVpcConnectorUsesSubnet,
			targetType:       awscloud.ResourceTypeEC2Subnet,
			sourceResourceID: vpcConnectorARN,
			targetResourceID: "subnet-aaa",
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerVpcConnectorUsesSecurityGroup,
			targetType:       awscloud.ResourceTypeEC2SecurityGroup,
			sourceResourceID: vpcConnectorARN,
			targetResourceID: "sg-111",
		},
		{
			relationshipType: awscloud.RelationshipAppRunnerVpcIngressConnectionTargetsService,
			targetType:       awscloud.ResourceTypeAppRunnerService,
			sourceResourceID: vpcIngressARN,
			targetResourceID: serviceARN,
		},
	}
	for _, tc := range cases {
		relationships := relationshipsByType(t, envelopes, tc.relationshipType)
		if len(relationships) == 0 {
			t.Fatalf("relationship %q not emitted", tc.relationshipType)
		}
		match := false
		for _, relationship := range relationships {
			if relationship["target_type"] == "" {
				t.Fatalf("relationship %q has empty target_type", tc.relationshipType)
			}
			if relationship["source_resource_id"] == tc.sourceResourceID &&
				relationship["target_resource_id"] == tc.targetResourceID {
				if relationship["target_type"] != tc.targetType {
					t.Fatalf("relationship %q target_type = %q, want %q", tc.relationshipType, relationship["target_type"], tc.targetType)
				}
				match = true
			}
		}
		if !match {
			t.Fatalf("relationship %q missing %q -> %q", tc.relationshipType, tc.sourceResourceID, tc.targetResourceID)
		}
	}
}

// TestServiceNeverPersistsEnvironmentValuesOrCredentials is the security
// acceptance gate. The scanner records environment-variable NAMES only and
// never the source repository credentials or environment-variable values.
func TestServiceNeverPersistsEnvironmentValuesOrCredentials(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	services := resourcesByType(t, envelopes, awscloud.ResourceTypeAppRunnerService)
	var imageService map[string]any
	for _, service := range services {
		if service["resource_id"] == serviceARN {
			imageService = service
		}
	}
	if imageService == nil {
		t.Fatalf("image service resource not found")
	}
	attributes, ok := imageService["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("service attributes payload missing")
	}
	names, ok := attributes["environment_variable_names"].([]string)
	if !ok {
		t.Fatalf("environment_variable_names = %#v, want []string", attributes["environment_variable_names"])
	}
	if len(names) != 2 || names[0] != "APP_MODE" {
		t.Fatalf("environment_variable_names = %v, want [APP_MODE LOG_LEVEL]", names)
	}
	for _, forbidden := range []string{"environment_variables", "runtime_environment_variables", "environment_values"} {
		if _, present := attributes[forbidden]; present {
			t.Fatalf("service attribute %q must not be present", forbidden)
		}
	}
}

// TestSourceConfigurationTypeAndHealthCheckPersisted confirms the source-config
// type and health check are recorded on the service resource.
func TestSourceConfigurationTypeAndHealthCheckPersisted(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	services := resourcesByType(t, envelopes, awscloud.ResourceTypeAppRunnerService)
	var service map[string]any
	for _, candidate := range services {
		if candidate["resource_id"] == serviceARN {
			service = candidate
		}
	}
	if service == nil {
		t.Fatalf("service resource not found")
	}
	if service["state"] != "RUNNING" {
		t.Fatalf("service state = %v, want RUNNING", service["state"])
	}
	attributes := service["attributes"].(map[string]any)
	if attributes["source_configuration_type"] != "IMAGE" {
		t.Fatalf("source_configuration_type = %v, want IMAGE", attributes["source_configuration_type"])
	}
	if attributes["auto_scaling_configuration_arn"] != autoScalingARN {
		t.Fatalf("auto_scaling_configuration_arn = %v, want %q", attributes["auto_scaling_configuration_arn"], autoScalingARN)
	}
	healthCheck, ok := attributes["health_check"].(map[string]any)
	if !ok {
		t.Fatalf("health_check payload missing")
	}
	if healthCheck["protocol"] != "HTTP" || healthCheck["path"] != "/health" {
		t.Fatalf("health_check = %#v, want HTTP /health", healthCheck)
	}
}

// TestSecretReferenceClassification confirms SSM-vs-SecretsManager target typing
// is derived from the ARN service segment.
func TestSecretReferenceClassification(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	relationships := relationshipsByType(t, envelopes, awscloud.RelationshipAppRunnerServiceReferencesSecret)
	seen := map[string]string{}
	for _, relationship := range relationships {
		seen[relationship["target_resource_id"].(string)] = relationship["target_type"].(string)
	}
	if seen[secretARN] != awscloud.ResourceTypeSecretsManagerSecret {
		t.Fatalf("secret %q target_type = %q, want %q", secretARN, seen[secretARN], awscloud.ResourceTypeSecretsManagerSecret)
	}
	if seen[ssmSecretARN] != awscloud.ResourceTypeSSMParameter {
		t.Fatalf("secret %q target_type = %q, want %q", ssmSecretARN, seen[ssmSecretARN], awscloud.ResourceTypeSSMParameter)
	}
}

// TestNoMutationLeakInPayload is a defense-in-depth sweep that no obvious
// credential-bearing substring leaks into any emitted payload.
func TestNoMutationLeakInPayload(t *testing.T) {
	scanner := apprunner.Scanner{Client: sampleClient()}
	envelopes, err := scanner.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, envelope := range envelopes {
		for key := range envelope.Payload {
			if strings.Contains(strings.ToLower(key), "password") ||
				strings.Contains(strings.ToLower(key), "credential") {
				t.Fatalf("payload key %q looks credential-bearing", key)
			}
		}
	}
}
