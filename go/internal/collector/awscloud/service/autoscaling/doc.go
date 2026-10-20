// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package autoscaling maps EC2 Auto Scaling observations into AWS cloud fact
// envelopes.
//
// The package owns scanner-level Auto Scaling fact selection for Auto Scaling
// groups, launch configurations, scaling policies, lifecycle hooks, scheduled
// actions, and their relationships. It is metadata-only: it never reads or
// persists launch configuration or launch template UserData (which can hold
// bootstrap secrets), and it never mutates an Auto Scaling resource. The Auto
// Scaling group resource_id is the bare group name so the CodeDeploy and Batch
// dangling edges that target aws_autoscaling_group by name resolve to this
// resource. AWS SDK pagination, credentials, persistence, graph projection,
// and reducer-owned correlation live outside this package.
package autoscaling
