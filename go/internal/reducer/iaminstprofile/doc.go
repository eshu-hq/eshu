// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iaminstprofile projects IAM instance-profile aws_resource
// role_arns into canonical HAS_ROLE edges from instance-profile
// CloudResource nodes to the IAM role CloudResource nodes they attach
// (issue #1299).
//
// The domain is additive and gates on the cloud_resource_uid
// canonical-nodes-committed phase before loading facts: a miss is retryable
// ([IAMInstanceProfileRoleNodesNotReadyFailureClass]), never an edge write
// against an endpoint set that has not committed. The handler never creates
// a CloudResource node, only resolves each role_arn to a scanned role node
// uid through a bounded in-memory ARN join index; an unscanned role is a
// conservative skip, never a fabricated node. A profile with no roles
// produces no edge and is not counted as a skip.
//
// Rows are deduplicated by (profile uid, HAS_ROLE, role uid) and sorted, so
// a batched write is byte-stable and idempotent across retries and
// reprojections. Every written and retracted edge is stamped with
// [IAMInstanceProfileRoleEvidenceSource] so the prior-generation retract
// scopes its delete to reducer-owned edges.
//
// The exported surface is [MaterializationDomainDefinition],
// [IAMInstanceProfileRoleEdgeWriter],
// [IAMInstanceProfileRoleMaterializationHandler],
// [ExtractIAMInstanceProfileRoleEdgeRows],
// [IAMInstanceProfileRoleEvidenceSource], and
// [IAMInstanceProfileRoleNodesNotReadyFailureClass]. This package never
// imports internal/reducer.
package iaminstprofile
