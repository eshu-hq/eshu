// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAutoScaling,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	groups               []Group
	launchConfigurations []LaunchConfiguration
	policies             []ScalingPolicy
	scheduledActions     []ScheduledAction
	hooksByGroup         map[string][]LifecycleHook
	err                  error
}

func (f fakeClient) ListAutoScalingGroups(context.Context) ([]Group, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.groups, nil
}

func (f fakeClient) ListLaunchConfigurations(context.Context) ([]LaunchConfiguration, error) {
	return f.launchConfigurations, nil
}

func (f fakeClient) ListScalingPolicies(context.Context) ([]ScalingPolicy, error) {
	return f.policies, nil
}

func (f fakeClient) ListScheduledActions(context.Context) ([]ScheduledAction, error) {
	return f.scheduledActions, nil
}

func (f fakeClient) ListLifecycleHooks(_ context.Context, group Group) ([]LifecycleHook, error) {
	return f.hooksByGroup[group.Name], nil
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
		groups: []Group{{
			ARN:                    "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/checkout-asg",
			Name:                   "checkout-asg",
			MinSize:                1,
			MaxSize:                5,
			DesiredCapacity:        3,
			AvailabilityZones:      []string{"us-east-1a", "us-east-1b"},
			HealthCheckType:        "ELB",
			HealthCheckGracePeriod: 300,
			Status:                 "",
			CapacityRebalance:      true,
			DefaultCooldown:        300,
			LaunchTemplateID:       "lt-0abc123",
			LaunchTemplateName:     "checkout-lt",
			LaunchTemplateVersion:  "$Latest",
			SubnetIDs:              []string{"subnet-aaa", "subnet-bbb"},
			TargetGroupARNs:        []string{"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/checkout-tg/abc123"},
			LoadBalancerNames:      []string{"checkout-clb"},
			ServiceLinkedRoleARN:   "arn:aws:iam::123456789012:role/aws-service-role/autoscaling.amazonaws.com/AWSServiceRoleForAutoScaling",
			TerminationPolicies:    []string{"Default"},
			CreatedTime:            time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
			Tags:                   map[string]string{"Team": "payments"},
		}, {
			ARN:                     "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid2:autoScalingGroupName/legacy-asg",
			Name:                    "legacy-asg",
			MinSize:                 0,
			MaxSize:                 2,
			DesiredCapacity:         1,
			AvailabilityZones:       []string{"us-east-1a"},
			HealthCheckType:         "EC2",
			LaunchConfigurationName: "legacy-lc",
			SubnetIDs:               []string{"subnet-ccc"},
		}},
		launchConfigurations: []LaunchConfiguration{{
			ARN:  "arn:aws:autoscaling:us-east-1:123456789012:launchConfiguration:uuid:launchConfigurationName/legacy-lc",
			Name: "legacy-lc",
		}},
		policies: []ScalingPolicy{{
			ARN:                  "arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:uuid:autoScalingGroupName/checkout-asg:policyName/scale-out",
			Name:                 "scale-out",
			AutoScalingGroupName: "checkout-asg",
			PolicyType:           "TargetTrackingScaling",
			AdjustmentType:       "ChangeInCapacity",
			Enabled:              true,
		}},
		scheduledActions: []ScheduledAction{{
			ARN:                  "arn:aws:autoscaling:us-east-1:123456789012:scheduledUpdateGroupAction:uuid:autoScalingGroupName/checkout-asg:scheduledActionName/scale-up-morning",
			Name:                 "scale-up-morning",
			AutoScalingGroupName: "checkout-asg",
			Recurrence:           "0 8 * * *",
			TimeZone:             "America/New_York",
			DesiredCapacity:      int32Ptr(5),
		}},
		hooksByGroup: map[string][]LifecycleHook{
			"checkout-asg": {{
				Name:                  "drain-hook",
				AutoScalingGroupName:  "checkout-asg",
				LifecycleTransition:   "autoscaling:EC2_INSTANCE_TERMINATING",
				DefaultResult:         "CONTINUE",
				HeartbeatTimeout:      300,
				GlobalTimeout:         3600,
				NotificationTargetARN: "arn:aws:sns:us-east-1:123456789012:asg-drain",
				RoleARN:               "arn:aws:iam::123456789012:role/asg-lifecycle",
			}},
		},
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func TestScannerEmitsGroupsLaunchConfigsPoliciesHooksAndScheduledActions(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	groups := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingGroup)
	if len(groups) != 2 {
		t.Fatalf("Auto Scaling group resources = %d, want 2", len(groups))
	}

	// The Auto Scaling group resource_id must be the bare group name so the
	// CodeDeploy and Batch dangling edges that target aws_autoscaling_group by
	// name resolve to this resource.
	var checkout map[string]any
	for _, group := range groups {
		if group["name"] == "checkout-asg" {
			checkout = group
		}
	}
	if checkout == nil {
		t.Fatalf("checkout-asg group not found")
	}
	if got := checkout["resource_id"]; got != "checkout-asg" {
		t.Fatalf("group resource_id = %v, want bare name checkout-asg", got)
	}
	attrs := checkout["attributes"].(map[string]any)
	if got := attrs["min_size"]; got != int32(1) {
		t.Fatalf("group min_size = %v, want 1", got)
	}
	if got := attrs["max_size"]; got != int32(5) {
		t.Fatalf("group max_size = %v, want 5", got)
	}
	if got := attrs["desired_capacity"]; got != int32(3) {
		t.Fatalf("group desired_capacity = %v, want 3", got)
	}
	if got := attrs["health_check_type"]; got != "ELB" {
		t.Fatalf("group health_check_type = %v, want ELB", got)
	}
	if got := attrs["capacity_rebalance"]; got != true {
		t.Fatalf("group capacity_rebalance = %v, want true", got)
	}

	launchConfigs := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingLaunchConfiguration)
	if len(launchConfigs) != 1 {
		t.Fatalf("launch configuration resources = %d, want 1", len(launchConfigs))
	}
	if got := launchConfigs[0]["resource_id"]; got != "legacy-lc" {
		t.Fatalf("launch configuration resource_id = %v, want legacy-lc", got)
	}

	policies := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingPolicy)
	if len(policies) != 1 {
		t.Fatalf("scaling policy resources = %d, want 1", len(policies))
	}

	hooks := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingLifecycleHook)
	if len(hooks) != 1 {
		t.Fatalf("lifecycle hook resources = %d, want 1", len(hooks))
	}
	if got := hooks[0]["resource_id"]; got != "checkout-asg/drain-hook" {
		t.Fatalf("lifecycle hook resource_id = %v, want checkout-asg/drain-hook", got)
	}

	actions := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingScheduledAction)
	if len(actions) != 1 {
		t.Fatalf("scheduled action resources = %d, want 1", len(actions))
	}
}

func TestScannerLaunchConfigurationEmitsNoAttributes(t *testing.T) {
	// Launch configuration facts must carry identity only. No attributes block
	// is emitted, so UserData and other launch detail can never be persisted.
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	launchConfigs := resourcesByType(t, envelopes, awscloud.ResourceTypeAutoScalingLaunchConfiguration)
	if len(launchConfigs) != 1 {
		t.Fatalf("launch configuration resources = %d, want 1", len(launchConfigs))
	}
	if attrs, ok := launchConfigs[0]["attributes"]; ok && attrs != nil {
		if m, isMap := attrs.(map[string]any); isMap && len(m) > 0 {
			t.Fatalf("launch configuration attributes = %v, want none (no UserData or launch detail)", m)
		}
	}
}

func TestScannerEmitsGroupRelationshipsWithGreppedJoinKeys(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// asg -> launch template: lt ID matches the EC2 launch-template resource_id
	// form (lt-...).
	lt := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingGroupUsesLaunchTemplate)
	if len(lt) != 1 {
		t.Fatalf("launch-template relationships = %d, want 1", len(lt))
	}
	assertEdge(t, lt[0], "checkout-asg", "lt-0abc123", awscloud.ResourceTypeEC2LaunchTemplate)

	// asg -> launch configuration: keyed on launch configuration name.
	lc := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingGroupUsesLaunchConfiguration)
	if len(lc) != 1 {
		t.Fatalf("launch-configuration relationships = %d, want 1", len(lc))
	}
	assertEdge(t, lc[0], "legacy-asg", "legacy-lc", awscloud.ResourceTypeAutoScalingLaunchConfiguration)

	// asg -> subnet: bare subnet IDs matching the EC2-owned subnet resource_id.
	subnets := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingGroupUsesSubnet)
	if len(subnets) != 3 {
		t.Fatalf("subnet relationships = %d, want 3", len(subnets))
	}
	for _, edge := range subnets {
		if got := edge["target_type"]; got != awscloud.ResourceTypeEC2Subnet {
			t.Fatalf("subnet edge target_type = %v, want %s", got, awscloud.ResourceTypeEC2Subnet)
		}
		target := edge["target_resource_id"].(string)
		if len(target) < 7 || target[:7] != "subnet-" {
			t.Fatalf("subnet edge target_resource_id = %v, want bare subnet-id", target)
		}
	}

	// asg -> target group: target group ARN matching the ELBv2-owned
	// target-group resource_id.
	tg := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingGroupAttachedToTargetGroup)
	if len(tg) != 1 {
		t.Fatalf("target-group relationships = %d, want 1", len(tg))
	}
	assertEdge(t, tg[0], "checkout-asg",
		"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/checkout-tg/abc123",
		awscloud.ResourceTypeELBv2TargetGroup)

	// asg -> service-linked IAM role: role ARN.
	role := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingGroupUsesIAMRole)
	if len(role) != 1 {
		t.Fatalf("IAM role relationships = %d, want 1", len(role))
	}
	assertEdge(t, role[0], "checkout-asg",
		"arn:aws:iam::123456789012:role/aws-service-role/autoscaling.amazonaws.com/AWSServiceRoleForAutoScaling",
		awscloud.ResourceTypeIAMRole)
}

func TestScannerEmitsChildToGroupRelationships(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	policy := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingPolicyTargetsGroup)
	if len(policy) != 1 {
		t.Fatalf("policy->group relationships = %d, want 1", len(policy))
	}
	if got := policy[0]["target_resource_id"]; got != "checkout-asg" {
		t.Fatalf("policy->group target_resource_id = %v, want checkout-asg", got)
	}
	if got := policy[0]["target_type"]; got != awscloud.ResourceTypeAutoScalingGroup {
		t.Fatalf("policy->group target_type = %v, want %s", got, awscloud.ResourceTypeAutoScalingGroup)
	}

	hook := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingLifecycleHookTargetsGroup)
	if len(hook) != 1 {
		t.Fatalf("hook->group relationships = %d, want 1", len(hook))
	}
	if got := hook[0]["target_resource_id"]; got != "checkout-asg" {
		t.Fatalf("hook->group target_resource_id = %v, want checkout-asg", got)
	}

	action := relationshipsByType(t, envelopes, awscloud.RelationshipAutoScalingScheduledActionTargetsGroup)
	if len(action) != 1 {
		t.Fatalf("action->group relationships = %d, want 1", len(action))
	}
	if got := action[0]["target_resource_id"]; got != "checkout-asg" {
		t.Fatalf("action->group target_resource_id = %v, want checkout-asg", got)
	}
}

func TestScannerAllRelationshipsHaveNonEmptyTargetType(t *testing.T) {
	envelopes, err := Scanner{Client: sampleClient()}.Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got := env.Payload["target_type"]; got == nil || got == "" {
			t.Fatalf("relationship %v has empty target_type", env.Payload["relationship_type"])
		}
	}
}

func TestScannerPropagatesClientError(t *testing.T) {
	wantErr := errors.New("describe failed")
	_, err := Scanner{Client: fakeClient{err: wantErr}}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want wrapped client error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Scan() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestScannerRejectsForeignServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceBatch
	_, err := Scanner{Client: sampleClient()}.Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service_kind rejection")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func assertEdge(t *testing.T, edge map[string]any, sourceID, targetID, targetType string) {
	t.Helper()
	if got := edge["source_resource_id"]; got != sourceID {
		t.Fatalf("edge source_resource_id = %v, want %s", got, sourceID)
	}
	if got := edge["target_resource_id"]; got != targetID {
		t.Fatalf("edge target_resource_id = %v, want %s", got, targetID)
	}
	if got := edge["target_type"]; got != targetType {
		t.Fatalf("edge target_type = %v, want %s", got, targetType)
	}
}
