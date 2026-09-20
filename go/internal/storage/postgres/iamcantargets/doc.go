// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iamcantargets is the Postgres implementation of
// iamcan.CrossScopeTargetLoader (#6785).
//
// The awscloud collector writes IAM roles and aws_iam_permission facts into the
// IAM service scope and every CAN_PERFORM catalog target (S3 bucket, KMS key,
// secret, parameter, table, instance, database, function) into its own service
// scope. [Store] lets the CAN_PERFORM handler resolve exact target ARNs from
// the sibling scopes of the same AWS account. It samples each candidate
// scope's readiness in one query (active generation, committed
// cloud_resource_uid nodes, pending generation), then reads only the requested
// ARNs pinned to the active generation that sample saw. Reads are bounded by
// account, requested service kind and region, and exact ARN; the package never
// scans another account or the whole fact table.
package iamcantargets
