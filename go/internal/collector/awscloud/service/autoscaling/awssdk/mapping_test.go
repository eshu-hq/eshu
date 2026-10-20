// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsautoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

// TestMapGroupSplitsSubnetsAndPrefersLaunchTemplateID proves the adapter parses
// the comma-separated VPCZoneIdentifier into bare subnet IDs and carries the
// launch template ID, matching the EC2-owned join key forms.
func TestMapGroupSplitsSubnetsAndPrefersLaunchTemplateID(t *testing.T) {
	group := mapGroup(awsautoscalingtypes.AutoScalingGroup{
		AutoScalingGroupName: aws.String("checkout-asg"),
		AutoScalingGroupARN:  aws.String("arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/checkout-asg"),
		MinSize:              aws.Int32(1),
		MaxSize:              aws.Int32(5),
		DesiredCapacity:      aws.Int32(3),
		HealthCheckType:      aws.String("ELB"),
		CapacityRebalance:    aws.Bool(true),
		VPCZoneIdentifier:    aws.String("subnet-aaa, subnet-bbb ,subnet-ccc"),
		LaunchTemplate: &awsautoscalingtypes.LaunchTemplateSpecification{
			LaunchTemplateId:   aws.String("lt-0abc123"),
			LaunchTemplateName: aws.String("checkout-lt"),
			Version:            aws.String("$Latest"),
		},
		TargetGroupARNs:      []string{"arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/checkout-tg/abc123"},
		ServiceLinkedRoleARN: aws.String("arn:aws:iam::123456789012:role/aws-service-role/autoscaling.amazonaws.com/AWSServiceRoleForAutoScaling"),
		Tags: []awsautoscalingtypes.TagDescription{{
			Key:   aws.String("Team"),
			Value: aws.String("payments"),
		}},
	})

	if group.Name != "checkout-asg" {
		t.Fatalf("group Name = %q, want checkout-asg", group.Name)
	}
	wantSubnets := []string{"subnet-aaa", "subnet-bbb", "subnet-ccc"}
	if !reflect.DeepEqual(group.SubnetIDs, wantSubnets) {
		t.Fatalf("group SubnetIDs = %v, want %v", group.SubnetIDs, wantSubnets)
	}
	if group.LaunchTemplateID != "lt-0abc123" {
		t.Fatalf("group LaunchTemplateID = %q, want lt-0abc123", group.LaunchTemplateID)
	}
	if group.Tags["Team"] != "payments" {
		t.Fatalf("group Tags[Team] = %q, want payments", group.Tags["Team"])
	}
}

// TestMapLaunchConfigurationDropsUserData proves the adapter never carries
// launch configuration UserData across the boundary, even when AWS reports it.
// UserData can hold bootstrap secrets; the scanner-owned type has no field for
// it, so a leak would not compile.
func TestMapLaunchConfigurationDropsUserData(t *testing.T) {
	launchConfiguration := mapLaunchConfiguration(awsautoscalingtypes.LaunchConfiguration{
		LaunchConfigurationName: aws.String("legacy-lc"),
		LaunchConfigurationARN:  aws.String("arn:aws:autoscaling:us-east-1:123456789012:launchConfiguration:uuid:launchConfigurationName/legacy-lc"),
		UserData:                aws.String("IyEvYmluL2Jhc2gKZXhwb3J0IFNFQ1JFVD1odW50ZXIy"),
		KeyName:                 aws.String("prod-key"),
		IamInstanceProfile:      aws.String("arn:aws:iam::123456789012:instance-profile/legacy"),
		SecurityGroups:          []string{"sg-123"},
	})

	if launchConfiguration.Name != "legacy-lc" {
		t.Fatalf("launch configuration Name = %q, want legacy-lc", launchConfiguration.Name)
	}
	// The scanner-owned LaunchConfiguration type carries identity only. Reflect
	// over it to prove no field can carry UserData or other launch detail.
	typ := reflect.TypeOf(launchConfiguration)
	if typ.NumField() != 2 {
		t.Fatalf("LaunchConfiguration has %d fields, want 2 (ARN, Name) so UserData can never be carried", typ.NumField())
	}
}

// TestMapLifecycleHookDropsNotificationMetadata proves the adapter never
// carries the caller-supplied NotificationMetadata across the boundary.
func TestMapLifecycleHookDropsNotificationMetadata(t *testing.T) {
	hook := mapLifecycleHook(awsautoscalingtypes.LifecycleHook{
		LifecycleHookName:     aws.String("drain-hook"),
		AutoScalingGroupName:  aws.String("checkout-asg"),
		LifecycleTransition:   aws.String("autoscaling:EC2_INSTANCE_TERMINATING"),
		DefaultResult:         aws.String("CONTINUE"),
		HeartbeatTimeout:      aws.Int32(300),
		NotificationMetadata:  aws.String("free-form caller data that must not be persisted"),
		NotificationTargetARN: aws.String("arn:aws:sns:us-east-1:123456789012:asg-drain"),
		RoleARN:               aws.String("arn:aws:iam::123456789012:role/asg-lifecycle"),
	})

	if hook.Name != "drain-hook" {
		t.Fatalf("hook Name = %q, want drain-hook", hook.Name)
	}
	if hook.LifecycleTransition != "autoscaling:EC2_INSTANCE_TERMINATING" {
		t.Fatalf("hook LifecycleTransition = %q, unexpected", hook.LifecycleTransition)
	}
	typ := reflect.TypeOf(hook)
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Name == "NotificationMetadata" {
			t.Fatalf("LifecycleHook declares NotificationMetadata; the field must be absent")
		}
	}
}

// TestMapScheduledActionCarriesScheduleAndCapacity proves the adapter maps the
// schedule and optional target capacity of a scheduled action.
func TestMapScheduledActionCarriesScheduleAndCapacity(t *testing.T) {
	action := mapScheduledAction(awsautoscalingtypes.ScheduledUpdateGroupAction{
		ScheduledActionName:  aws.String("scale-up-morning"),
		ScheduledActionARN:   aws.String("arn:aws:autoscaling:us-east-1:123456789012:scheduledUpdateGroupAction:uuid:autoScalingGroupName/checkout-asg:scheduledActionName/scale-up-morning"),
		AutoScalingGroupName: aws.String("checkout-asg"),
		Recurrence:           aws.String("0 8 * * *"),
		TimeZone:             aws.String("America/New_York"),
		DesiredCapacity:      aws.Int32(5),
	})

	if action.Name != "scale-up-morning" {
		t.Fatalf("action Name = %q, want scale-up-morning", action.Name)
	}
	if action.DesiredCapacity == nil || *action.DesiredCapacity != 5 {
		t.Fatalf("action DesiredCapacity = %v, want 5", action.DesiredCapacity)
	}
	if action.AutoScalingGroupName != "checkout-asg" {
		t.Fatalf("action AutoScalingGroupName = %q, want checkout-asg", action.AutoScalingGroupName)
	}
}
