// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Application Auto Scaling observations for
// one AWS claim. Implementations read control-plane metadata through the
// Application Auto Scaling Describe APIs across every supported service
// namespace and never register, deregister, mutate, or invoke a scaling
// action.
type Client interface {
	// Snapshot returns every scalable target, scaling policy, and scheduled
	// action visible to the configured AWS credentials, iterating each
	// supported service namespace.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Application Auto Scaling metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// ScalableTargets is the metadata-only set of registered scalable targets.
	ScalableTargets []ScalableTarget
	// ScalingPolicies is the metadata-only set of scaling policies.
	ScalingPolicies []ScalingPolicy
	// ScheduledActions is the metadata-only set of scheduled actions.
	ScheduledActions []ScheduledAction
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component for one namespace.
	Warnings []awscloud.WarningObservation
}

// ScalableTarget is the scanner-owned Application Auto Scaling scalable target
// model. It carries control-plane metadata only.
type ScalableTarget struct {
	// ARN is the scalable target ARN AWS reports, when present.
	ARN string
	// ServiceNamespace is the AWS service namespace the target belongs to (for
	// example ecs, dynamodb, rds, lambda).
	ServiceNamespace string
	// ResourceID is the service-reported identifier of the scaled resource (for
	// example service/my-cluster/my-service or table/my-table).
	ResourceID string
	// ScalableDimension is the scalable dimension (for example
	// ecs:service:DesiredCount).
	ScalableDimension string
	// RoleARN is the IAM role ARN Application Auto Scaling assumes to modify the
	// target on the customer's behalf.
	RoleARN string
	// MinCapacity is the minimum capacity the target scales in to.
	MinCapacity *int32
	// MaxCapacity is the maximum capacity the target scales out to.
	MaxCapacity *int32
	// SuspendedDynamicScalingInSuspended reports whether dynamic scale-in is
	// suspended.
	SuspendedDynamicScalingInSuspended *bool
	// SuspendedDynamicScalingOutSuspended reports whether dynamic scale-out is
	// suspended.
	SuspendedDynamicScalingOutSuspended *bool
	// SuspendedScheduledScalingSuspended reports whether scheduled scaling is
	// suspended.
	SuspendedScheduledScalingSuspended *bool
	// CreationTime is when the scalable target was registered.
	CreationTime time.Time
}

// ScalingPolicy is the scanner-owned Application Auto Scaling scaling policy
// model. The step-scaling and target-tracking configuration bodies are
// intentionally excluded; only the bound CloudWatch alarm identities are kept.
type ScalingPolicy struct {
	// ARN is the scaling policy ARN.
	ARN string
	// Name is the scaling policy name.
	Name string
	// PolicyType is the policy type (for example StepScaling,
	// TargetTrackingScaling, PredictiveScaling).
	PolicyType string
	// ServiceNamespace is the owning AWS service namespace.
	ServiceNamespace string
	// ResourceID is the service-reported identifier of the scaled resource.
	ResourceID string
	// ScalableDimension is the scalable dimension the policy governs.
	ScalableDimension string
	// AlarmARNs are the ARNs of the CloudWatch alarms the policy is bound to.
	AlarmARNs []string
	// CreationTime is when the scaling policy was created.
	CreationTime time.Time
}

// ScheduledAction is the scanner-owned Application Auto Scaling scheduled
// action model. It carries control-plane metadata only.
type ScheduledAction struct {
	// ARN is the scheduled action ARN.
	ARN string
	// Name is the scheduled action name.
	Name string
	// ServiceNamespace is the owning AWS service namespace.
	ServiceNamespace string
	// ResourceID is the service-reported identifier of the scaled resource.
	ResourceID string
	// ScalableDimension is the scalable dimension the action governs.
	ScalableDimension string
	// Schedule is the schedule expression (at/rate/cron).
	Schedule string
	// Timezone is the time zone the schedule is evaluated in, when set.
	Timezone string
	// MinCapacity is the minimum capacity the action sets, when present.
	MinCapacity *int32
	// MaxCapacity is the maximum capacity the action sets, when present.
	MaxCapacity *int32
	// StartTime is when the action's window opens, when set.
	StartTime time.Time
	// EndTime is when the action's window closes, when set.
	EndTime time.Time
	// CreationTime is when the scheduled action was created.
	CreationTime time.Time
}
