// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import (
	"context"
	"time"
)

// Client is the AWS EC2 Auto Scaling read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned types. The
// surface is metadata-only: it exposes no CreateAutoScalingGroup,
// UpdateAutoScalingGroup, DeleteAutoScalingGroup, SetDesiredCapacity,
// TerminateInstanceInAutoScalingGroup, or any other Create/Update/Delete/Set
// operation, and it never reads launch configuration or launch template
// UserData.
type Client interface {
	ListAutoScalingGroups(context.Context) ([]Group, error)
	ListLaunchConfigurations(context.Context) ([]LaunchConfiguration, error)
	ListScalingPolicies(context.Context) ([]ScalingPolicy, error)
	ListScheduledActions(context.Context) ([]ScheduledAction, error)
	// ListLifecycleHooks returns the lifecycle hooks defined on one Auto Scaling
	// group. AWS reports lifecycle hooks per group, so the adapter fans this out
	// per discovered group.
	ListLifecycleHooks(context.Context, Group) ([]LifecycleHook, error)
}

// Group is the scanner-owned representation of one EC2 Auto Scaling group. It
// carries inventory and topology metadata only. The instance list and warm
// pool detail AWS reports are intentionally absent so per-instance churn never
// becomes graph truth.
type Group struct {
	ARN                     string
	Name                    string
	MinSize                 int32
	MaxSize                 int32
	DesiredCapacity         int32
	AvailabilityZones       []string
	HealthCheckType         string
	HealthCheckGracePeriod  int32
	Status                  string
	CapacityRebalance       bool
	DefaultCooldown         int32
	NewInstancesProtected   bool
	MaxInstanceLifetime     int32
	LaunchConfigurationName string
	LaunchTemplateID        string
	LaunchTemplateName      string
	LaunchTemplateVersion   string
	SubnetIDs               []string
	TargetGroupARNs         []string
	LoadBalancerNames       []string
	ServiceLinkedRoleARN    string
	TerminationPolicies     []string
	CreatedTime             time.Time
	Tags                    map[string]string
}

// LaunchConfiguration is the scanner-owned representation of one EC2 Auto
// Scaling launch configuration. Only identity is carried. UserData,
// BlockDeviceMappings, SecurityGroups, KeyName, and IamInstanceProfile are
// intentionally absent from this type so they can never be persisted; UserData
// in particular can hold bootstrap secrets.
type LaunchConfiguration struct {
	ARN  string
	Name string
}

// ScalingPolicy is the scanner-owned representation of one EC2 Auto Scaling
// scaling policy. It carries policy identity, type, and the owning group name
// only. The step adjustments, target-tracking configuration, and CloudWatch
// alarm bindings AWS reports are intentionally absent.
type ScalingPolicy struct {
	ARN                  string
	Name                 string
	AutoScalingGroupName string
	PolicyType           string
	AdjustmentType       string
	Enabled              bool
}

// LifecycleHook is the scanner-owned representation of one EC2 Auto Scaling
// lifecycle hook. The NotificationMetadata field AWS reports is intentionally
// absent because it can carry caller-supplied free-form data; only the
// transition, target, role, and timeout metadata are carried.
type LifecycleHook struct {
	Name                  string
	AutoScalingGroupName  string
	LifecycleTransition   string
	DefaultResult         string
	HeartbeatTimeout      int32
	GlobalTimeout         int32
	NotificationTargetARN string
	RoleARN               string
}

// ScheduledAction is the scanner-owned representation of one EC2 Auto Scaling
// scheduled action. It carries the schedule and target capacity metadata only.
type ScheduledAction struct {
	ARN                  string
	Name                 string
	AutoScalingGroupName string
	Recurrence           string
	TimeZone             string
	MinSize              *int32
	MaxSize              *int32
	DesiredCapacity      *int32
	StartTime            time.Time
	EndTime              time.Time
}
