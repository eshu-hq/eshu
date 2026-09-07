// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2usesprofile projects ec2_instance_posture instance_profile_arn
// into canonical USES_PROFILE edges from EC2 instance CloudResource nodes to
// the IAM instance-profile CloudResource nodes they use (issue #1146 PR-B).
//
// The domain is additive and gates on BOTH endpoint node phases before loading
// facts: the IAM instance-profile target phase published by aws_resource
// materialization and the EC2 instance source phase published by EC2 instance
// node materialization. A miss on either is retryable
// ([EC2UsesProfileNodesNotReadyFailureClass]), never an edge write against an
// endpoint set that has not committed. The handler never creates a
// CloudResource node, only resolves each instance_profile_arn to a scanned
// profile node uid through a bounded in-memory ARN join index; an unscanned
// profile is a conservative skip, not a fabricated node. A blank
// instance_profile_arn (no attached profile) and a tombstoned instance produce
// no row and are not counted as skips.
//
// Rows are deduplicated by (source uid, USES_PROFILE, target uid) and sorted,
// so a batched write is byte-stable and idempotent across retries and
// reprojections.
//
// The exported surface is [MaterializationDomainDefinition],
// [EC2UsesProfileEdgeWriter], [EC2UsesProfileMaterializationHandler],
// [ExtractEC2UsesProfileEdgeRows], and
// [EC2UsesProfileNodesNotReadyFailureClass]. This package never imports
// internal/reducer.
package ec2usesprofile
