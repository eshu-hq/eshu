// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package docdb emits metadata-only Amazon DocumentDB resource and
// relationship facts for the AWS cloud collector.
//
// The package owns scanner-level fact selection for DB clusters, cluster
// instances, cluster parameter groups (name, family, and parameter count
// only), cluster snapshot metadata, subnet groups, global clusters, and event
// subscriptions. It deliberately avoids database connections, master user
// passwords and secrets, snapshot contents, database documents, collections,
// indexes, and cluster parameter values. AWS SDK pagination and API telemetry
// live in the awssdk adapter.
package docdb
