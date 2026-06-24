// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package s3 maps Amazon S3 bucket metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence bucket resources, derived
// metadata-only s3_bucket_posture facts, bounded s3_external_principal_grant
// facts, normalized aws_resource_policy_permission facts, and relationships for
// server-access-log delivery targets. The posture fact carries
// block-public-access flags, default-encryption detail, versioning and
// MFA-delete state, object-ownership / ACL-disabled state, access-logging
// target, replication presence, and booleans derived from the bucket policy
// document. External-principal grant facts carry only public, cross-account,
// AWS service, or unsupported-principal metadata. The
// aws_resource_policy_permission fact is the normalized, derived projection of
// each bucket-policy statement (effect, normalized action/resource patterns,
// condition key/operator NAMES, and derived grantee principal facts) — the
// resource-side analog of aws_iam_permission (PR4b of #1134). Object inventory, the raw
// bucket policy JSON, statement bodies, statement Sids, condition VALUES, ACL
// grants, replication rule detail, lifecycle rules, notification configuration,
// and mutation APIs stay outside this package contract.
package s3
