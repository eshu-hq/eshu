// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package s3logsto projects s3_bucket_posture logging-target facts onto
// canonical LOGS_TO edges between S3 CloudResource nodes (issue #1144 PR2).
//
// The domain is additive and gates on the cloud_resource_uid canonical-nodes
// phase published by aws_resource materialization: a miss is retryable
// ([S3LogsToNodesNotReadyFailureClass]), never an edge write against a node
// set that has not committed. The handler never creates a CloudResource node,
// only matches by uid; a posture whose source bucket or log-target bucket was
// not scanned in the same scope generation is a conservative skip, not a
// fabricated node.
//
// A posture fact only produces a row when both its own bucket and its
// logging_target_bucket resolve through the shared bucket-name join index in
// [cloudjoin]; a blank logging target (logging disabled) is the normal
// no-edge state, not a skip. Rows are deduplicated by (source uid, LOGS_TO,
// target uid) and sorted, so a batched write is byte-stable and idempotent
// across retries and reprojections.
//
// The exported surface is [MaterializationDomainDefinition],
// [S3LogsToEdgeWriter], [S3LogsToMaterializationHandler],
// [ExtractS3LogsToEdgeRows], and [S3LogsToNodesNotReadyFailureClass]. This
// package never imports internal/reducer.
package s3logsto
