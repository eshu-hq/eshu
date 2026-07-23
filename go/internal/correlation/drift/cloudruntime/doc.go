// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudruntime carries helper Go for the AWS cloud-runtime drift
// correlation pack.
//
// The package compares one ARN's AWS-observed resource, Terraform-state
// resource, and Terraform-config resource views before
// engine.Evaluate(rules.AWSCloudRuntimeDriftRulePack(), ...) runs. It emits
// candidates for existence findings -- cloud resources with no
// Terraform-state backing, cloud resources that have state backing but no
// current config declaration, and unresolved/conflicting ownership evidence
// -- plus, once all three layers agree the resource is Terraform-managed, a
// value-drift finding (image_version_drift, #5453) when an allowlisted
// comparable value (AMI, Lambda image URI or version, or an ECS
// task-definition container image) differs between the AWS-observed
// resource and the Terraform-declared state. It does not write graph truth
// or query any backend; reducer wiring decides when to persist or publish
// the evaluation.
package cloudruntime
