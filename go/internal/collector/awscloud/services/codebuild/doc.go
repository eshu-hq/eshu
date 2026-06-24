// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codebuild maps AWS CodeBuild metadata into AWS cloud collector facts.
//
// The package owns scanner-level normalization only. It never calls the AWS
// SDK directly, never reads or persists buildspec.yml bodies, never persists
// environment-variable PLAINTEXT values, never persists build logs, and never
// persists source-credential tokens. SDK adapters supply scanner-owned records
// with PLAINTEXT environment values already redacted, and Scanner emits
// aws_resource facts for build projects, report groups, and recent builds plus
// aws_relationship facts for the project edges CodeBuild reports directly
// (IAM role, VPC/subnet/security-group, KMS key, S3 or Git source, S3
// artifacts, Secrets Manager and SSM Parameter Store references). Scan fails
// closed when the redaction key is zero so secret-shaped values cannot leak.
package codebuild
