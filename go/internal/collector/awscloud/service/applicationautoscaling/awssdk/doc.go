// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Application Auto Scaling client
// into the metadata-only applicationautoscaling scanner interface.
//
// The adapter uses DescribeScalableTargets, DescribeScalingPolicies, and
// DescribeScheduledActions, fanning out across every supported service
// namespace, to read scalable target, scaling policy, and scheduled action
// control-plane metadata. It intentionally excludes RegisterScalableTarget,
// DeregisterScalableTarget, PutScalingPolicy, DeleteScalingPolicy,
// PutScheduledAction, DeleteScheduledAction, and every scaling-action API, so
// the adapter cannot mutate or invoke scaling state. A namespace throttled after
// SDK retries records a non-fatal sustained-throttle warning and is skipped
// rather than failing the whole scan.
package awssdk
