// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awscloudruntimedrift builds the AWS cloud-runtime-drift reducer
// intent from one immutable scope generation: when the generation carries any
// aws_resource fact, it asks the reducer to run the bounded AWS ARN join
// against active Terraform-state and Terraform-config facts and re-classify
// runtime drift for the scope (issue #6053 epic). The projector stays
// source-local — it never joins AWS resources to Terraform evidence itself —
// so aws_resource presence alone is enough to enqueue the reducer's own join.
// The intent is anchored to the first aws_resource fact in original
// generation order so the reducer claim is stable across reprojections of the
// same generation. Root projector assembly owns lookup construction and
// lifetime, invocation order, queue writes, retries, and telemetry.
package awscloudruntimedrift
