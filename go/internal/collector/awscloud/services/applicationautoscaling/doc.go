// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package applicationautoscaling maps AWS Application Auto Scaling scalable
// target, scaling policy, and scheduled action metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence resources for scalable targets, scaling
// policies, and scheduled actions across every supported service namespace
// (dynamodb, ecs, rds, lambda, and others), plus relationships that join a
// scalable target to the resource it governs (DynamoDB table, ECS service,
// Aurora cluster, Lambda function), a scaling policy to its bound CloudWatch
// alarms, and policies/scheduled actions to the scalable target they act on.
// Scale edges are emitted only for namespaces whose governed resource resolves
// to a scanned, ARN-keyed node; an unresolvable namespace emits the target node
// but no dangling edge. Step-scaling and target-tracking configuration bodies
// are never persisted, and no register, deregister, put, delete, or
// scaling-action API is reachable: the scanner is metadata-only.
package applicationautoscaling
