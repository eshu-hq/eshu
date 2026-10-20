// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Compute Optimizer client into the
// metadata-only Compute Optimizer scanner interface.
//
// The adapter uses GetRecommendationSummaries, GetEC2InstanceRecommendations,
// GetAutoScalingGroupRecommendations, GetEBSVolumeRecommendations, and
// GetLambdaFunctionRecommendations to read Compute Optimizer recommendation
// control-plane metadata. It intentionally excludes UpdateEnrollmentStatus, the
// recommendation-preference mutation APIs, and every export-start API, so the
// adapter cannot mutate Compute Optimizer state or read the CloudWatch
// utilization metric data points behind a recommendation. An account that is not
// opted in to Compute Optimizer yields an empty snapshot rather than an error.
package awssdk
