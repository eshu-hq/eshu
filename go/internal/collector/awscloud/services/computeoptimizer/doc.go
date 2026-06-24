// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package computeoptimizer maps AWS Compute Optimizer recommendation metadata
// into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for recommendation summaries
// (one per resource type) and per-resource recommendations for EC2 instances,
// Auto Scaling groups, EBS volumes, and Lambda functions, plus
// recommendation-to-target relationships to the analyzed EC2 instance (bare
// instance id), Auto Scaling group (group name), and Lambda function (function
// ARN). EBS volume recommendations currently carry no edge in this scanner; the
// volume identity is recorded as metadata until recommendation-to-volume
// relationship projection lands separately. The scanner is metadata-only: it
// never mutates Compute Optimizer state, never changes enrollment, and never
// persists the CloudWatch utilization metric data points behind a
// recommendation.
package computeoptimizer
