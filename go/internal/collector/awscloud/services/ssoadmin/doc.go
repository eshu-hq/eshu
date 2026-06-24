// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ssoadmin maps AWS IAM Identity Center metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level Identity Center normalization only. It never
// calls the AWS SDK directly and never persists permission set inline policy
// bodies, permissions boundary bodies, customer-managed policy bodies, or
// application access-scope filters. The permission set inline policy encodes
// the org least-privilege model and is deliberately outside this contract.
//
// SDK adapters provide a Snapshot, and Scanner emits aws_resource facts for
// instances, permission sets, account assignments, applications, trusted token
// issuers, and resolved principals, plus aws_relationship evidence between
// them. Principal display names are redacted through awscloud.RedactString
// before persistence, so Scanner requires a non-zero redaction key.
package ssoadmin
