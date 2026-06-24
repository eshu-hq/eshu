// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sagemaker maps AWS SageMaker control-plane metadata into AWS cloud
// collector facts.
//
// The scanner emits metadata-only facts for the SageMaker resource surface:
// notebook instances, models, endpoints, endpoint configurations, training
// jobs, processing jobs, transform jobs, hyperparameter tuning jobs, projects,
// pipelines, feature groups, Studio domains, user profiles, apps, and
// inference components. It also reports model-to-S3-artifact,
// model-to-container-image, model-to-IAM-role, endpoint-to-endpoint-config,
// endpoint-config-to-model, training-job-to-IAM-role, notebook-to-subnet,
// domain-to-VPC, and user-profile-to-domain relationships when AWS reports
// ARN- or ID-shaped identities.
//
// Inference is outside the scanner contract: InvokeEndpoint and
// InvokeEndpointAsync live in the separate sagemakerruntime module, which this
// package never imports, so the model endpoints can never be called. The
// scanner is also payload-blind by construction — hyperparameter values,
// training/processing/transform data references, notebook lifecycle-config
// script bodies, container environment maps, and pipeline definition bodies
// are never copied into scanner-owned types. SDK-call safety is enforced by
// the reflection guard in the awssdk adapter, which fails the build if a
// mutation or Invoke* method reaches the adapter-local apiClient interface.
package sagemaker
