// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk/types"

	ebservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticbeanstalk"
)

// TestElasticBeanstalkAPIClientInterfaceExcludesMutationAndDataPlaneAPIs proves
// the AWS SDK Elastic Beanstalk surface this adapter accepts never lists an
// application/environment mutation, an environment rebuild/terminate, a CNAME
// swap, or an environment-info data-plane reader as a callable method. It is the
// reflective guard the issue requires first: a maintainer cannot widen the
// metadata-only contract to reach mutation or data-plane APIs without failing
// this test.
func TestElasticBeanstalkAPIClientInterfaceExcludesMutationAndDataPlaneAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Application / version / template / environment mutation.
		"CreateApplication",
		"UpdateApplication",
		"DeleteApplication",
		"CreateApplicationVersion",
		"UpdateApplicationVersion",
		"DeleteApplicationVersion",
		"UpdateApplicationResourceLifecycle",
		"CreateConfigurationTemplate",
		"UpdateConfigurationTemplate",
		"DeleteConfigurationTemplate",
		"DeleteEnvironmentConfiguration",
		"CreateEnvironment",
		"UpdateEnvironment",
		"TerminateEnvironment",
		"RebuildEnvironment",
		"RestartAppServer",
		"AbortEnvironmentUpdate",
		"ApplyEnvironmentManagedAction",
		"ComposeEnvironments",
		"SwapEnvironmentCNAMEs",
		"CreatePlatformVersion",
		"DeletePlatformVersion",
		"CreateStorageLocation",
		"AssociateEnvironmentOperationsRole",
		"DisassociateEnvironmentOperationsRole",
		"UpdateTagsForResource",
		// Environment-info data plane (presigned-URL bundle readers).
		"RequestEnvironmentInfo",
		"RetrieveEnvironmentInfo",
		// Validation surface that echoes secret option values back.
		"ValidateConfigurationSettings",
	}
	for _, method := range forbidden {
		if _, ok := clientType.MethodByName(method); ok {
			t.Fatalf("apiClient exposes forbidden method %q; Elastic Beanstalk SDK adapter must stay metadata-only", method)
		}
	}
}

func TestMapEnvironmentMapsMetadataTierAndHealth(t *testing.T) {
	created := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	environment := mapEnvironment(ebtypes.EnvironmentDescription{
		EnvironmentArn:    aws.String("arn:aws:elasticbeanstalk:us-east-1:123456789012:environment/checkout/checkout-prod"),
		EnvironmentId:     aws.String("e-abc123"),
		EnvironmentName:   aws.String("checkout-prod"),
		ApplicationName:   aws.String("checkout"),
		CNAME:             aws.String("checkout-prod.us-east-1.elasticbeanstalk.com"),
		EndpointURL:       aws.String("awseb-checkout.us-east-1.elb.amazonaws.com"),
		Status:            ebtypes.EnvironmentStatusReady,
		Health:            ebtypes.EnvironmentHealthGreen,
		HealthStatus:      ebtypes.EnvironmentHealthStatusOk,
		PlatformArn:       aws.String("arn:aws:elasticbeanstalk:us-east-1::platform/Python 3.11 running on 64bit Amazon Linux 2023/4.0.0"),
		SolutionStackName: aws.String("64bit Amazon Linux 2023 v4.0.0 running Python 3.11"),
		TemplateName:      aws.String("checkout-template"),
		VersionLabel:      aws.String("v42"),
		OperationsRole:    aws.String("arn:aws:iam::123456789012:role/eb-ops"),
		DateCreated:       aws.Time(created),
		Tier: &ebtypes.EnvironmentTier{
			Name: aws.String("WebServer"),
			Type: aws.String("Standard"),
		},
	})

	if environment.ARN != "arn:aws:elasticbeanstalk:us-east-1:123456789012:environment/checkout/checkout-prod" {
		t.Fatalf("ARN = %q", environment.ARN)
	}
	if environment.ID != "e-abc123" {
		t.Fatalf("ID = %q", environment.ID)
	}
	if environment.Status != "Ready" {
		t.Fatalf("Status = %q, want Ready", environment.Status)
	}
	if environment.Health != "Green" {
		t.Fatalf("Health = %q, want Green", environment.Health)
	}
	if environment.HealthStatus != "Ok" {
		t.Fatalf("HealthStatus = %q, want Ok", environment.HealthStatus)
	}
	if environment.TierName != "WebServer" || environment.TierType != "Standard" {
		t.Fatalf("tier = %q/%q, want WebServer/Standard", environment.TierName, environment.TierType)
	}
	if environment.CNAME != "checkout-prod.us-east-1.elasticbeanstalk.com" {
		t.Fatalf("CNAME = %q", environment.CNAME)
	}
}

func TestMapEnvironmentResourcesCollectsJoinIdentities(t *testing.T) {
	resources := mapEnvironmentResources(&ebtypes.EnvironmentResourceDescription{
		AutoScalingGroups: []ebtypes.AutoScalingGroup{{Name: aws.String("awseb-e-abc-stack-AWSEBAutoScalingGroup")}},
		LaunchTemplates:   []ebtypes.LaunchTemplate{{Id: aws.String("lt-0123456789abcdef0")}},
		LoadBalancers:     []ebtypes.LoadBalancer{{Name: aws.String("awseb-AWSEB-LB")}},
		// A nil-name member must be dropped, not panic.
		Instances: []ebtypes.Instance{{Id: aws.String("i-123")}},
	})

	if len(resources.AutoScalingGroupNames) != 1 || resources.AutoScalingGroupNames[0] != "awseb-e-abc-stack-AWSEBAutoScalingGroup" {
		t.Fatalf("auto scaling groups = %#v", resources.AutoScalingGroupNames)
	}
	if len(resources.LaunchTemplateIDs) != 1 || resources.LaunchTemplateIDs[0] != "lt-0123456789abcdef0" {
		t.Fatalf("launch templates = %#v", resources.LaunchTemplateIDs)
	}
	if len(resources.LoadBalancerNames) != 1 || resources.LoadBalancerNames[0] != "awseb-AWSEB-LB" {
		t.Fatalf("load balancers = %#v", resources.LoadBalancerNames)
	}
}

func TestMapOptionSettingsPreservesValuesForScannerRedaction(t *testing.T) {
	settings := mapOptionSettings([]ebtypes.ConfigurationOptionSetting{
		{
			Namespace:  aws.String("aws:elasticbeanstalk:application:environment"),
			OptionName: aws.String("DATABASE_URL"),
			Value:      aws.String("postgres://user:password@db.internal/app"),
		},
		{
			Namespace:  aws.String("aws:ec2:vpc"),
			OptionName: aws.String("VPCId"),
			Value:      aws.String("vpc-0123456789abcdef0"),
		},
	})

	if len(settings) != 2 {
		t.Fatalf("settings = %#v, want 2", settings)
	}
	if settings[0].Value != "postgres://user:password@db.internal/app" {
		t.Fatalf("env value was not preserved for scanner redaction: %q", settings[0].Value)
	}
	if settings[1].Value != "vpc-0123456789abcdef0" {
		t.Fatalf("vpc id was not preserved: %q", settings[1].Value)
	}
}

var _ ebservice.Client = (*Client)(nil)
