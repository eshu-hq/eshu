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
// resource and the Terraform-declared state.
//
// A fourth case sits between drift and convergence: a resource type value
// drift covers, for which this pass cannot speak: either not one comparable
// value could be compared, or the comparisons that ran agreed while another
// covered comparable was unreadable (#5861). (A shape value drift can never
// pair -- an ECS task definition with more than one observed image -- is NOT
// this: it reads the same on every pass, so it is uncovered rather than
// degraded.) That is
// value_comparison_inconclusive (#5837), not silence. Silence means
// convergence to the caller, which drops the ARN from the candidate set and
// lets the reducer's generation-authoritative retire DELETE a still-true drift
// finding; an explicit uncertainty finding keeps the ARN durable and
// self-heals once the evidence returns. ClassifyValueComparison is the single
// authority separating "compared and agreed" from "could not compare".
//
// It does not write graph truth or query any backend; reducer wiring decides
// when to persist or publish the evaluation.
package cloudruntime
