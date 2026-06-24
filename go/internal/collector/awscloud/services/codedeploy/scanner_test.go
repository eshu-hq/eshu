// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeDeploy,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC),
	}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("codedeploy-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

type fakeClient struct {
	applications      []Application
	groupsByApp       map[string][]DeploymentGroup
	deploymentConfigs []DeploymentConfig
	deployments       []Deployment
}

func (f fakeClient) ListApplications(context.Context) ([]Application, error) {
	return f.applications, nil
}

func (f fakeClient) ListDeploymentGroups(_ context.Context, app string) ([]DeploymentGroup, error) {
	return f.groupsByApp[app], nil
}

func (f fakeClient) ListDeploymentConfigs(context.Context) ([]DeploymentConfig, error) {
	return f.deploymentConfigs, nil
}

func (f fakeClient) ListRecentDeployments(context.Context) ([]Deployment, error) {
	return f.deployments, nil
}

func resourcesByType(t *testing.T, envelopes []facts.Envelope, resourceType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if env.Payload["resource_type"] == resourceType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relType string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if env.Payload["relationship_type"] == relType {
			matched = append(matched, env.Payload)
		}
	}
	return matched
}

func sampleClient() fakeClient {
	return fakeClient{
		applications: []Application{{
			Name:            "checkout",
			ID:              "app-123",
			ComputePlatform: "Server",
			CreateTime:      time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
			Tags:            map[string]string{"Team": "payments"},
		}},
		groupsByApp: map[string][]DeploymentGroup{
			"checkout": {{
				Name:                 "checkout-prod",
				ID:                   "dg-456",
				ApplicationName:      "checkout",
				ComputePlatform:      "Server",
				DeploymentConfigName: "CodeDeployDefault.OneAtATime",
				ServiceRoleARN:       "arn:aws:iam::123456789012:role/CodeDeployServiceRole",
				DeploymentStyle: DeploymentStyle{
					DeploymentType:   "BLUE_GREEN",
					DeploymentOption: "WITH_TRAFFIC_CONTROL",
				},
				AutoRollback: AutoRollbackConfig{
					Enabled: true,
					Events:  []string{"DEPLOYMENT_FAILURE"},
				},
				OutdatedInstancesStrategy: "UPDATE",
				AutoScalingGroups:         []string{"checkout-asg"},
				ECSServices: []ECSServiceTarget{{
					ClusterName: "checkout-cluster",
					ServiceName: "checkout-svc",
				}},
				LambdaFunctions: []string{"checkout-canary"},
				SNSTriggers: []SNSTrigger{{
					Name:     "prod-alerts",
					TopicARN: "arn:aws:sns:us-east-1:123456789012:codedeploy-alerts",
					Events:   []string{"DeploymentFailure"},
				}},
				EC2TagFilterSummary: []TagFilterSummary{{
					Key:      "Environment",
					Type:     "KEY_AND_VALUE",
					HasValue: true,
				}},
				Tags: map[string]string{"Team": "payments"},
			}},
		},
		deploymentConfigs: []DeploymentConfig{{
			Name:                    "ThreeAtATime",
			ID:                      "dc-789",
			ComputePlatform:         "Server",
			MinimumHealthyHostType:  "HOST_COUNT",
			MinimumHealthyHostValue: 3,
			CreateTime:              time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
		}},
		deployments: []Deployment{{
			ID:                   "d-ABCDE1234",
			ApplicationName:      "checkout",
			DeploymentGroupName:  "checkout-prod",
			DeploymentConfigName: "CodeDeployDefault.OneAtATime",
			Status:               "Succeeded",
			Creator:              "user",
			ComputePlatform:      "Server",
			CreateTime:           time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
			CompleteTime:         time.Date(2026, time.May, 1, 0, 5, 0, 0, time.UTC),
			RevisionSummary: RevisionSummary{
				RevisionType: "S3",
				S3Bucket:     "checkout-artifacts",
				S3Key:        "releases/checkout-1.2.3.zip",
				S3Version:    "v42",
				S3BundleType: "zip",
			},
		}},
	}
}

func TestScannerEmitsApplicationsGroupsConfigsAndDeployments(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	apps := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeDeployApplication)
	if len(apps) != 1 {
		t.Fatalf("application resources = %d, want 1", len(apps))
	}
	if got := apps[0]["name"]; got != "checkout" {
		t.Fatalf("application name = %v, want checkout", got)
	}
	appAttrs := apps[0]["attributes"].(map[string]any)
	if got := appAttrs["compute_platform"]; got != "Server" {
		t.Fatalf("application compute_platform = %v, want Server", got)
	}

	groups := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeDeployDeploymentGroup)
	if len(groups) != 1 {
		t.Fatalf("deployment-group resources = %d, want 1", len(groups))
	}
	groupAttrs := groups[0]["attributes"].(map[string]any)
	style := groupAttrs["deployment_style"].(map[string]any)
	if style["deployment_type"] != "BLUE_GREEN" {
		t.Fatalf("deployment_style.deployment_type = %v, want BLUE_GREEN", style["deployment_type"])
	}
	rollback := groupAttrs["auto_rollback"].(map[string]any)
	if rollback["enabled"] != true {
		t.Fatalf("auto_rollback.enabled = %v, want true", rollback["enabled"])
	}

	configs := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeDeployDeploymentConfig)
	if len(configs) != 1 {
		t.Fatalf("deployment-config resources = %d, want 1", len(configs))
	}
	configAttrs := configs[0]["attributes"].(map[string]any)
	if got := configAttrs["minimum_healthy_host_value"]; got != int32(3) {
		t.Fatalf("minimum_healthy_host_value = %v, want 3", got)
	}

	deployments := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeDeployDeployment)
	if len(deployments) != 1 {
		t.Fatalf("deployment resources = %d, want 1", len(deployments))
	}
	depAttrs := deployments[0]["attributes"].(map[string]any)
	if got := depAttrs["status"]; got != "Succeeded" {
		t.Fatalf("deployment status = %v, want Succeeded", got)
	}
	revision := depAttrs["revision_summary"].(map[string]any)
	if revision["s3_bucket"] != "checkout-artifacts" {
		t.Fatalf("revision_summary.s3_bucket = %v, want checkout-artifacts", revision["s3_bucket"])
	}
}

func TestScannerEmitsDeploymentGroupRelationships(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	app := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupBelongsToApplication)
	if len(app) != 1 {
		t.Fatalf("group->application relationships = %d, want 1", len(app))
	}
	if app[0]["target_type"] != awscloud.ResourceTypeCodeDeployApplication {
		t.Fatalf("group->application target_type = %v", app[0]["target_type"])
	}

	role := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupUsesIAMRole)
	if len(role) != 1 {
		t.Fatalf("group->IAM-role relationships = %d, want 1", len(role))
	}
	if role[0]["target_arn"] != "arn:aws:iam::123456789012:role/CodeDeployServiceRole" {
		t.Fatalf("group->IAM-role target_arn = %v", role[0]["target_arn"])
	}

	asg := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupTargetsAutoScalingGroup)
	if len(asg) != 1 {
		t.Fatalf("group->ASG relationships = %d, want 1", len(asg))
	}
	if asg[0]["target_resource_id"] != "checkout-asg" {
		t.Fatalf("group->ASG target_resource_id = %v", asg[0]["target_resource_id"])
	}

	ecs := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupTargetsECSService)
	if len(ecs) != 1 {
		t.Fatalf("group->ECS relationships = %d, want 1", len(ecs))
	}
	// The ECS scanner emits its service resource_id as the service ARN, so the
	// CodeDeploy edge must target that same ARN to join the ECS service node.
	const wantECSARN = "arn:aws:ecs:us-east-1:123456789012:service/checkout-cluster/checkout-svc"
	if ecs[0]["target_resource_id"] != wantECSARN {
		t.Fatalf("group->ECS target_resource_id = %v, want %v", ecs[0]["target_resource_id"], wantECSARN)
	}
	if ecs[0]["target_arn"] != wantECSARN {
		t.Fatalf("group->ECS target_arn = %v, want %v", ecs[0]["target_arn"], wantECSARN)
	}
	ecsAttrs := ecs[0]["attributes"].(map[string]any)
	if ecsAttrs["cluster_name"] != "checkout-cluster" || ecsAttrs["service_name"] != "checkout-svc" {
		t.Fatalf("group->ECS attributes = %#v, want cluster/service names preserved", ecsAttrs)
	}

	lambda := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupTargetsLambdaFunction)
	if len(lambda) != 1 {
		t.Fatalf("group->Lambda relationships = %d, want 1", len(lambda))
	}
	// The Lambda scanner emits its function resource_id as the function ARN, so
	// the CodeDeploy edge must target that same ARN to join the function node.
	const wantLambdaARN = "arn:aws:lambda:us-east-1:123456789012:function:checkout-canary"
	if lambda[0]["target_resource_id"] != wantLambdaARN {
		t.Fatalf("group->Lambda target_resource_id = %v, want %v", lambda[0]["target_resource_id"], wantLambdaARN)
	}
	if lambda[0]["target_arn"] != wantLambdaARN {
		t.Fatalf("group->Lambda target_arn = %v, want %v", lambda[0]["target_arn"], wantLambdaARN)
	}

	sns := relationshipsByType(t, envelopes, awscloud.RelationshipCodeDeployDeploymentGroupNotifiesSNSTopic)
	if len(sns) != 1 {
		t.Fatalf("group->SNS relationships = %d, want 1", len(sns))
	}
	if sns[0]["target_arn"] != "arn:aws:sns:us-east-1:123456789012:codedeploy-alerts" {
		t.Fatalf("group->SNS target_arn = %v", sns[0]["target_arn"])
	}
}

func TestScannerRedactsOnPremisesTagFilterValues(t *testing.T) {
	client := sampleClient()
	key := testKey(t)
	marker := awscloud.RedactString("john.doe@example.com", "codedeploy_on_premises_tag_value", key)
	group := client.groupsByApp["checkout"][0]
	group.OnPremisesTagFilterSummary = []TagFilterSummary{{
		Key:         "owner-email",
		Type:        "KEY_AND_VALUE",
		HasValue:    true,
		ValueMarker: marker,
	}}
	client.groupsByApp["checkout"] = []DeploymentGroup{group}

	envelopes, err := Scanner{Client: client, RedactionKey: key}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	groups := resourcesByType(t, envelopes, awscloud.ResourceTypeCodeDeployDeploymentGroup)
	if len(groups) != 1 {
		t.Fatalf("deployment-group resources = %d, want 1", len(groups))
	}
	attrs := groups[0]["attributes"].(map[string]any)
	filters := attrs["on_premises_tag_filters"].([]map[string]any)
	if len(filters) != 1 {
		t.Fatalf("on_premises_tag_filters = %d, want 1", len(filters))
	}
	if filters[0]["key"] != "owner-email" {
		t.Fatalf("filter key = %v, want owner-email", filters[0]["key"])
	}
	value, ok := filters[0]["value"].(map[string]any)
	if !ok {
		t.Fatalf("filter value is not a redaction marker: %#v", filters[0]["value"])
	}
	markerStr, ok := value["marker"].(string)
	if !ok || markerStr == "" {
		t.Fatalf("filter value marker missing: %#v", value)
	}
	if markerStr == "john.doe@example.com" {
		t.Fatalf("filter value leaked raw on-premises tag value")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{RedactionKey: testKey(t)}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want redaction-key-required error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	_, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service-kind mismatch error")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	envelopes, err := Scanner{Client: sampleClient(), RedactionKey: testKey(t)}.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, env := range envelopes {
		if env.Payload["service_kind"] != awscloud.ServiceCodeDeploy {
			t.Fatalf("service_kind = %v, want codedeploy", env.Payload["service_kind"])
		}
	}
}
