// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticbeanstalk

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	testEnvironmentARN  = "arn:aws:elasticbeanstalk:us-east-1:123456789012:environment/checkout/checkout-prod"
	testAppVersionARN   = "arn:aws:elasticbeanstalk:us-east-1:123456789012:applicationversion/checkout/v42"
	testInstanceProf    = "arn:aws:iam::123456789012:instance-profile/aws-elasticbeanstalk-ec2-role"
	testServiceRole     = "arn:aws:iam::123456789012:role/aws-elasticbeanstalk-service-role"
	testLoadBalancerARN = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/awseb-AWSEB-1A2B3C/0123456789abcdef"
)

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func fullClient() fakeClient {
	return fakeClient{
		applications: []Application{{
			ARN:                    "arn:aws:elasticbeanstalk:us-east-1:123456789012:application/checkout",
			Name:                   "checkout",
			ConfigurationTemplates: []string{"checkout-template", "blue-green"},
			VersionLabels:          []string{"v41", "v42"},
		}},
		environments: []Environment{{
			ARN:               testEnvironmentARN,
			ID:                "e-abc123",
			Name:              "checkout-prod",
			ApplicationName:   "checkout",
			Status:            "Ready",
			Health:            "Green",
			HealthStatus:      "Ok",
			TierName:          "WebServer",
			TierType:          "Standard",
			PlatformARN:       "arn:aws:elasticbeanstalk:us-east-1::platform/Python 3.11 running on 64bit Amazon Linux 2023/4.0.0",
			SolutionStackName: "64bit Amazon Linux 2023 v4.0.0 running Python 3.11",
			CNAME:             "checkout-prod.us-east-1.elasticbeanstalk.com",
			VersionLabel:      "v42",
			TemplateName:      "checkout-template",
		}},
		versions: []ApplicationVersion{{
			ARN:             testAppVersionARN,
			ApplicationName: "checkout",
			VersionLabel:    "v42",
			Status:          "Processed",
			SourceS3Bucket:  "elasticbeanstalk-us-east-1-123456789012",
			SourceS3Key:     "checkout/v42.zip",
		}},
		resources: map[string]EnvironmentResources{
			"e-abc123": {
				AutoScalingGroupNames: []string{"awseb-e-abc123-stack-AWSEBAutoScalingGroup"},
				LaunchTemplateIDs:     []string{"lt-0123456789abcdef0"},
				LoadBalancerNames:     []string{testLoadBalancerARN},
			},
		},
		settings: map[string][]OptionSetting{
			"checkout/checkout-prod": {
				{Namespace: "aws:ec2:vpc", OptionName: "VPCId", Value: "vpc-0123456789abcdef0"},
				{Namespace: "aws:autoscaling:launchconfiguration", OptionName: "IamInstanceProfile", Value: testInstanceProf},
				{Namespace: "aws:elasticbeanstalk:environment", OptionName: "ServiceRole", Value: testServiceRole},
				{Namespace: "aws:elasticbeanstalk:application:environment", OptionName: "DATABASE_URL", Value: "postgres://user:password@db.internal/app"},
				{Namespace: "aws:elasticbeanstalk:application:environment", OptionName: "FEATURE_FLAG", Value: "on"},
			},
		},
	}
}

func TestScannerEmitsApplicationsEnvironmentsAndVersions(t *testing.T) {
	envelopes, err := (Scanner{Client: fullClient(), RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	application := resourceByType(t, envelopes, awscloud.ResourceTypeElasticBeanstalkApplication)
	attrs := attributesOf(t, application)
	templates, ok := attrs["configuration_templates"].([]string)
	if !ok || len(templates) != 2 || templates[0] != "checkout-template" {
		t.Fatalf("configuration_templates = %#v, want [checkout-template blue-green]", attrs["configuration_templates"])
	}

	environment := resourceByType(t, envelopes, awscloud.ResourceTypeElasticBeanstalkEnvironment)
	envAttrs := attributesOf(t, environment)
	for key, want := range map[string]string{
		"status":              "Ready",
		"health":              "Green",
		"health_status":       "Ok",
		"tier_name":           "WebServer",
		"tier_type":           "Standard",
		"cname":               "checkout-prod.us-east-1.elasticbeanstalk.com",
		"solution_stack_name": "64bit Amazon Linux 2023 v4.0.0 running Python 3.11",
		"version_label":       "v42",
	} {
		if got, _ := envAttrs[key].(string); got != want {
			t.Fatalf("environment attribute %q = %q, want %q", key, got, want)
		}
	}
	if got := environment.Payload["arn"]; got != testEnvironmentARN {
		t.Fatalf("environment arn = %#v, want %q", got, testEnvironmentARN)
	}

	version := resourceByType(t, envelopes, awscloud.ResourceTypeElasticBeanstalkApplicationVersion)
	versionAttrs := attributesOf(t, version)
	if got, _ := versionAttrs["version_label"].(string); got != "v42" {
		t.Fatalf("version_label = %q, want v42", got)
	}
}

func TestScannerEmitsEnvironmentRelationshipsWithJoinKeys(t *testing.T) {
	envelopes, err := (Scanner{Client: fullClient(), RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	cases := []struct {
		relationship string
		targetID     string
		targetType   string
	}{
		{
			awscloud.RelationshipElasticBeanstalkEnvironmentBelongsToApplication,
			"arn:aws:elasticbeanstalk:us-east-1:123456789012:application/checkout",
			awscloud.ResourceTypeElasticBeanstalkApplication,
		},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesVPC, "vpc-0123456789abcdef0", awscloud.ResourceTypeEC2VPC},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile, testInstanceProf, awscloud.ResourceTypeIAMInstanceProfile},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesServiceRole, testServiceRole, awscloud.ResourceTypeIAMRole},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer, testLoadBalancerARN, awscloud.ResourceTypeELBv2LoadBalancer},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup, "awseb-e-abc123-stack-AWSEBAutoScalingGroup", awscloud.ResourceTypeAutoScalingGroup},
		{awscloud.RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate, "lt-0123456789abcdef0", awscloud.ResourceTypeEC2LaunchTemplate},
		{awscloud.RelationshipElasticBeanstalkEnvironmentRunsVersion, testAppVersionARN, awscloud.ResourceTypeElasticBeanstalkApplicationVersion},
	}
	for _, tc := range cases {
		relationship := relationshipByType(t, envelopes, tc.relationship)
		if got := relationship.Payload["source_resource_id"]; got != testEnvironmentARN {
			t.Fatalf("%s source_resource_id = %#v, want %q", tc.relationship, got, testEnvironmentARN)
		}
		if got, _ := relationship.Payload["target_resource_id"].(string); got != tc.targetID {
			t.Fatalf("%s target_resource_id = %q, want %q", tc.relationship, got, tc.targetID)
		}
		if got, _ := relationship.Payload["target_type"].(string); got != tc.targetType {
			t.Fatalf("%s target_type = %q, want %q", tc.relationship, got, tc.targetType)
		}
	}
}

// TestResourceRelationshipsTypesLoadBalancerByIdentifierShape proves the
// regression Copilot flagged: DescribeEnvironmentResources reports a load
// balancer identifier that is an ELBv2 ARN for ALB/NLB environments and a bare
// classic-ELB name for Classic environments. The ELBv2 scanner keys its nodes
// by ARN, so only an ARN-shaped ELBv2 identifier may claim the
// aws_elbv2_load_balancer target type (and then carry a real target_arn); any
// non-ARN identifier (a classic ELB name) must fall back to the generic
// aws_resource type and never fabricate an ARN, so the edge cannot mis-join an
// ELBv2 node it does not match.
func TestResourceRelationshipsTypesLoadBalancerByIdentifierShape(t *testing.T) {
	const (
		albARN  = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/awseb-AWSEB-1A2B3C/0123456789abcdef"
		clbName = "awseb-AWSEB-LB"
	)
	cases := []struct {
		name           string
		loadBalancer   string
		wantTargetID   string
		wantTargetType string
		wantTargetARN  string
	}{
		{
			name:           "alb arn keeps elbv2 type and sets target arn",
			loadBalancer:   albARN,
			wantTargetID:   albARN,
			wantTargetType: awscloud.ResourceTypeELBv2LoadBalancer,
			wantTargetARN:  albARN,
		},
		{
			name:           "classic elb name falls back to generic resource without arn",
			loadBalancer:   clbName,
			wantTargetID:   clbName,
			wantTargetType: "aws_resource",
			wantTargetARN:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observations := resourceRelationships(
				testBoundary(),
				testEnvironmentARN,
				testEnvironmentARN,
				EnvironmentResources{LoadBalancerNames: []string{tc.loadBalancer}},
			)
			var rel awscloud.RelationshipObservation
			var found bool
			for _, obs := range observations {
				if obs.RelationshipType == awscloud.RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer {
					rel = obs
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("no load-balancer relationship emitted for %q", tc.loadBalancer)
			}
			if rel.TargetResourceID != tc.wantTargetID {
				t.Fatalf("target_resource_id = %q, want %q", rel.TargetResourceID, tc.wantTargetID)
			}
			if rel.TargetType != tc.wantTargetType {
				t.Fatalf("target_type = %q, want %q", rel.TargetType, tc.wantTargetType)
			}
			if rel.TargetARN != tc.wantTargetARN {
				t.Fatalf("target_arn = %q, want %q", rel.TargetARN, tc.wantTargetARN)
			}
		})
	}
}

// TestScannerRedactsOptionSettingValuesAndNeverPersistsClearText is the
// security proof: the environment resource carries option-setting names but no
// clear-text secret value, and the secret-shaped value never appears anywhere
// in the emitted facts.
func TestScannerRedactsOptionSettingValuesAndNeverPersistsClearText(t *testing.T) {
	const secret = "postgres://user:password@db.internal/app"
	envelopes, err := (Scanner{Client: fullClient(), RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	environment := resourceByType(t, envelopes, awscloud.ResourceTypeElasticBeanstalkEnvironment)
	options, ok := attributesOf(t, environment)["option_settings"].([]map[string]any)
	if !ok {
		t.Fatalf("option_settings = %#v, want []map[string]any", attributesOf(t, environment)["option_settings"])
	}
	var sawDatabaseURL bool
	for _, option := range options {
		name, _ := option["name"].(string)
		if name == "DATABASE_URL" {
			sawDatabaseURL = true
			if option["value"] == secret {
				t.Fatalf("DATABASE_URL value persisted in clear text")
			}
			marker, ok := option["value"].(map[string]any)
			if !ok {
				t.Fatalf("DATABASE_URL value = %#v, want redaction marker map", option["value"])
			}
			if got, _ := marker["marker"].(string); !strings.HasPrefix(got, "redacted:") {
				t.Fatalf("DATABASE_URL marker = %q, want redacted: prefix", got)
			}
		}
		if _, isString := option["value"].(string); isString {
			t.Fatalf("option %q value persisted as raw string %q; all option values must be redaction markers", name, option["value"])
		}
	}
	if !sawDatabaseURL {
		t.Fatalf("DATABASE_URL option not present in option_settings")
	}

	// No envelope payload anywhere may contain the clear-text secret.
	for _, envelope := range envelopes {
		if strings.Contains(payloadString(t, envelope), secret) {
			t.Fatalf("clear-text secret leaked in fact %s", envelope.FactKind)
		}
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fullClient()}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want redaction key error")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scan() error = %q, want redaction key", err)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECS
	_, err := (Scanner{Client: fullClient(), RedactionKey: testKey(t)}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerStopsOnClientError(t *testing.T) {
	client := fullClient()
	client.environmentsErr = context.DeadlineExceeded
	_, err := (Scanner{Client: client, RedactionKey: testKey(t)}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want propagated client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceElasticBeanstalk,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:elasticbeanstalk:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	applications    []Application
	environments    []Environment
	versions        []ApplicationVersion
	resources       map[string]EnvironmentResources
	settings        map[string][]OptionSetting
	environmentsErr error
}

func (c fakeClient) DescribeApplications(context.Context) ([]Application, error) {
	return c.applications, nil
}

func (c fakeClient) DescribeEnvironments(context.Context) ([]Environment, error) {
	if c.environmentsErr != nil {
		return nil, c.environmentsErr
	}
	return c.environments, nil
}

func (c fakeClient) DescribeApplicationVersions(context.Context) ([]ApplicationVersion, error) {
	return c.versions, nil
}

func (c fakeClient) DescribeEnvironmentResources(_ context.Context, environmentID string) (EnvironmentResources, error) {
	return c.resources[environmentID], nil
}

func (c fakeClient) DescribeConfigurationSettings(_ context.Context, applicationName, environmentName string) ([]OptionSetting, error) {
	return c.settings[applicationName+"/"+environmentName], nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// payloadString renders a fact payload to a flat string with Go's deep
// formatting so a secret substring search scans every nested map and slice
// value, including redaction marker maps.
func payloadString(t *testing.T, envelope facts.Envelope) string {
	t.Helper()
	return fmt.Sprintf("%#v", envelope.Payload)
}
