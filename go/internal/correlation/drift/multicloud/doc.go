// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package multicloud carries provider-neutral cloud-runtime drift correlation
// helpers shared by AWS, GCP, and Azure (issues #1997, #1998).
//
// The package reuses the AWS cloud-runtime join logic in
// internal/correlation/drift/cloudruntime, but keys every finding on the
// canonical cloud_resource_uid keyspace owned by
// internal/correlation/cloudinventory instead of a provider-specific ARN. That
// keeps one drift path across providers: an observed cloud resource with no
// Terraform-state backing is orphaned, an observed resource with state but no
// config is unmanaged, conflicting deterministic ownership is ambiguous, a
// collector coverage gap is unknown, and -- once all three layers converge --
// an allowlisted comparable value that differs (AMI, Lambda image/version, or
// ECS container image) is image_version_drift (#5453). Provider observation
// never overwrites declared IaC truth; missing, ambiguous, and unknown states
// are carried as explicit evidence, never fabricated into a single owner.
//
// The package emits engine candidates for
// rules.MultiCloudRuntimeDriftRulePack(); reducer wiring decides when to persist
// or publish the evaluation. It does not write graph truth or query any backend.
package multicloud
