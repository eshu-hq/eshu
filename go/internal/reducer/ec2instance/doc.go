// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2instance materializes EC2 instances as canonical CloudResource
// graph nodes and augments them with identity properties (issue #1146 PR-A,
// issue #5448).
//
// The EC2 scanner deliberately does not emit an aws_resource inventory fact
// for instances (instances are high-cardinality and ephemeral), so the node
// domain ([EC2InstanceNodeMaterializationHandler]) is the only path that
// materializes an EC2 instance as a graph node: it projects
// ec2_instance_posture facts into deterministic node rows keyed by the
// canonical cloud_resource_uid and, after the write succeeds (or is a
// legitimate no-op for an empty generation), publishes the
// cloud_resource_uid / canonical_nodes_committed readiness phase under its
// own distinct entity key. The identity domain
// ([EC2InstanceIdentityMaterializationHandler]) gates on that exact phase and
// SETs only the disjoint ami_id property (retract-first per
// scope_id+evidence_source) onto the already-created node — never the base
// identity/posture fields the node domain owns, so the dual-domain write is
// safe to MERGE onto the same uid.
//
// Both handlers never fabricate graph state: a posture fact that carries
// neither an instance id nor an arn, a tombstoned instance, and (identity
// only) an instance with no observed ami_id produce no row. A malformed fact
// is quarantined per-fact (input_invalid) while every valid fact in the same
// batch still projects. Rows are deduplicated by uid under the #5007
// max-source_order_key rule and sorted, so a batched write is byte-stable
// and idempotent across retries and reprojections.
//
// The exported surface is [NodeMaterializationDomainDefinition],
// [IdentityMaterializationDomainDefinition],
// [EC2InstanceNodeMaterializationHandler],
// [EC2InstanceIdentityMaterializationHandler], [EC2InstanceNodeWriter],
// [EC2InstanceIdentityNodeWriter], [ExtractEC2InstanceNodeRows],
// [ExtractEC2InstanceNodeRowsWithSkips], [ExtractEC2InstanceIdentityNodeRows],
// and [EC2InstanceIdentityNodesNotReadyFailureClass]. This package never
// imports internal/reducer.
package ec2instance
