// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package backup maps AWS Backup service metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level Backup normalization only. It never calls the
// AWS SDK directly, never accesses recovery point contents, never persists
// backup vault access policy bodies, and never persists framework control
// input parameter values. SDK adapters provide vault, plan, selection,
// recovery point, report plan, restore testing plan, and framework
// metadata records. Scanner emits aws_resource facts plus relationship
// evidence for plan-to-selection, selection-to-resource, selection-to-role,
// vault-to-KMS-key, recovery-point-to-vault, recovery-point-to-source-resource,
// and framework-to-control links.
package backup
