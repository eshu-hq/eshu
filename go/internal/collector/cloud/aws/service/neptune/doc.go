// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package neptune emits metadata-only Amazon Neptune resource and relationship
// facts for the AWS cloud collector.
//
// The package owns scanner-level fact selection for Neptune (provisioned,
// RDS-shaped) DB clusters, cluster instances, cluster parameter groups (name
// and family only), cluster snapshot metadata, subnet groups, and global
// clusters, plus Neptune Analytics graphs (name, status, vector-search
// embedding dimension) and graph snapshot metadata. It deliberately avoids
// database connections, graph endpoints, master user passwords and secrets,
// snapshot contents, cluster parameter values, graph vertex or edge contents,
// graph query (ExecuteQuery) results, and import/export task payloads. AWS SDK
// pagination and API telemetry live in the awssdk adapter.
package neptune
