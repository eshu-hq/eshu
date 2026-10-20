// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package synthetics maps Amazon CloudWatch Synthetics canary metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence resources for Synthetics canaries plus
// relationships for the canary-to-S3-artifact-bucket, canary-to-IAM-execution-
// role, and (for VPC-configured canaries) canary-to-subnet and
// canary-to-security-group dependencies. Canary script source code, run
// artifacts (logs, screenshots, HAR files), run results, and every mutation or
// run-control API stay outside this package contract: the scanner is
// metadata-only.
package synthetics
