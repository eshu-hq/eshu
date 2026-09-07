// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package internetexposure derives conservative internet-exposure state for
// EC2 instances and S3 buckets and writes it as properties onto existing
// CloudResource graph nodes (issues #1301 and #1232).
//
// Two domains, one family:
//
//   - [EC2InternetExposureMaterializationHandler] (DomainEC2InternetExposure-
//     Materialization) projects ec2_instance_posture facts joined against ENI
//     relationship and security-group rule facts. A public IP alone never
//     means exposed: exposure requires an observed internet-ingress rule on a
//     security group attached (through an ENI) to the instance. Missing ENI,
//     SG, or rule evidence stays state=unknown, never a safe false, and raw
//     public IP addresses are never persisted.
//   - [S3InternetExposureMaterializationHandler] (DomainS3InternetExposure-
//     Materialization) projects s3_bucket_posture facts resolved through the
//     same scoped aws_resource S3 bucket join index LOGS_TO uses
//     ([cloudjoin.BuildS3BucketJoinIndex]). Posture whose source bucket did
//     not scan as an S3 CloudResource produces no row. Unknown or partial
//     posture stays state=unknown with a nil boolean, never a fabricated
//     false.
//
// Both handlers gate on the cloud_resource_uid / canonical_nodes_committed
// readiness phase before loading facts, retract prior-generation properties
// before writing, and never create CloudResource nodes: a missing uid is
// always a no-op. A malformed fact is quarantined per-fact (input_invalid)
// while every valid fact in the same batch still projects. Rows are
// deduplicated by uid and sorted, so a batched write is byte-stable and
// idempotent across retries and reprojections.
//
// The exported surface is
// [EC2InternetExposureMaterializationDomainDefinition],
// [S3InternetExposureMaterializationDomainDefinition],
// [EC2InternetExposureMaterializationHandler],
// [S3InternetExposureMaterializationHandler], [EC2InternetExposureNodeWriter],
// [S3InternetExposureNodeWriter], [ExtractEC2InternetExposureRows],
// [ExtractS3InternetExposureRows],
// [EC2InternetExposureNodesNotReadyFailureClass], and
// [S3InternetExposureNodesNotReadyFailureClass]. This package never imports
// internal/reducer.
package internetexposure
