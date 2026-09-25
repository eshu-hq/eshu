// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package securitylake maps Amazon Security Lake control-plane configuration
// into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Security Lake data lakes,
// log sources, and subscribers plus relationships for the data-lake-to-S3
// bucket, data-lake-to-KMS-key, data-lake-to-Lake-Formation registered
// resource, log-source-in-data-lake membership, log-source-to-IAM-role (custom
// source log provider), subscriber-to-IAM-role, and subscriber-to-S3 evidence.
// Ingested security log records, object contents, the subscriber external id,
// subscriber endpoints, and any credential material stay outside this package
// contract: the scanner is metadata-only.
package securitylake
