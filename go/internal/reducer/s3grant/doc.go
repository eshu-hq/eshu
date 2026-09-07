// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package s3grant projects metadata-only s3_external_principal_grant facts
// onto canonical GRANTS_ACCESS_TO edges from S3 CloudResource nodes to
// ExternalPrincipal nodes (issue #1231).
//
// The domain is additive and gates on the cloud_resource_uid canonical-nodes
// phase published by aws_resource materialization: a miss is retryable
// ([S3ExternalPrincipalGrantNodesNotReadyFailureClass]), never an edge write
// against a node set that has not committed. The handler never creates a
// CloudResource node, only matches the source bucket by uid through the shared
// bucket-name join index in [cloudjoin]; a grant whose source bucket was not
// scanned in the same scope generation is a conservative skip, not a
// fabricated node. Only bounded principal identity metadata and derived
// outcome booleans become row properties — never raw policy, statement, ACL,
// condition, or object fields.
//
// Rows are deduplicated by (source uid, principal uid) and sorted, so a
// batched write is byte-stable and idempotent across retries and
// reprojections.
//
// The exported surface is [MaterializationDomainDefinition],
// [S3ExternalPrincipalGrantWriter],
// [S3ExternalPrincipalGrantMaterializationHandler],
// [ExtractS3ExternalPrincipalGrantRows], and
// [S3ExternalPrincipalGrantNodesNotReadyFailureClass]. This package never
// imports internal/reducer.
package s3grant
