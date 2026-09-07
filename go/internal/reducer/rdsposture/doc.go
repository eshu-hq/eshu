// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rdsposture projects rds_instance_posture facts onto existing RDS
// CloudResource nodes as reducer-owned properties (issue #1233).
//
// The domain is additive and gates on the cloud_resource_uid canonical-nodes
// phase published by aws_resource materialization: a miss is retryable
// ([RDSPostureNodesNotReadyFailureClass]), never a property write against a
// node set that has not committed. The handler never creates a CloudResource
// node, only matches by uid; a posture whose RDS DB instance or Aurora
// cluster was not scanned as a CloudResource in the same scope generation is
// a conservative skip, not a fabricated node.
//
// A posture fact only produces a row when the same scope generation also
// emitted an aws_resource fact for the same RDS DB instance or Aurora
// cluster; a fact that fails this join is counted source-unresolved rather
// than projected with a guessed uid. Rows are deduplicated by CloudResource
// uid and sorted, so a batched write is byte-stable and idempotent across
// retries and reprojections.
//
// The exported surface is [MaterializationDomainDefinition],
// [RDSPostureNodeWriter], [RDSPostureMaterializationHandler],
// [ExtractRDSPostureRows], and [RDSPostureNodesNotReadyFailureClass]. This
// package never imports internal/reducer.
package rdsposture
