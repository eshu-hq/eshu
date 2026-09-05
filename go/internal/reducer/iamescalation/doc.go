// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iamescalation projects merged aws_iam_permission facts into
// conservative IAM CAN_ESCALATE_TO privilege-escalation edges.
//
// The handler gates on the cloud_resource_uid canonical-nodes-committed
// phase ([gpphase.KeyspaceCloudResourceUID] /
// [gpphase.PhaseCanonicalNodesCommitted]): a miss is retryable, never a
// fabricated edge against a node set that has not committed yet.
//
// A curated primitive arms only when every required action is present,
// Allow, unconditioned, free of NotAction/NotResource, and not Deny-touched
// -- Deny always wins over Allow. An armed primitive still only becomes an
// edge when its target resolves to EXACTLY ONE scanned IAM CloudResource
// node of the expected resource_type; wildcard, ambiguous (many matches),
// and zero (unresolved) targets all degrade to a counted skip and never
// fabricate a node. sts:AssumeRole is recognized but deferred to the
// separate CAN_ASSUME trust edge (#1134 PR2); this package never emits it.
//
// Edge rows are deduplicated by (principal_uid, target_uid) with the
// contributing primitives merged into one sorted primitives[] list, and the
// output rows are sorted, so the batched write is byte-stable and
// idempotent across retries and reprojections. A malformed identity fact is
// quarantined per-fact (input_invalid) while every valid fact in the same
// batch still projects.
//
// The exported surface is [MaterializationDomainDefinition],
// [IAMEscalationEdgeWriter], [IAMEscalationMaterializationHandler],
// [IAMEscalationResult], [ExtractIAMEscalationEdges], and
// [IAMEscalationNodesNotReadyFailureClass]. This package never imports
// internal/reducer.
package iamescalation
