// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package redshift emits metadata-only Amazon Redshift resource and
// relationship facts for the AWS cloud collector. It covers both provisioned
// Redshift and Redshift Serverless control planes.
//
// The package owns scanner-level fact selection for provisioned clusters,
// cluster parameter groups, cluster subnet groups, cluster snapshot metadata,
// scheduled action metadata, plus Redshift Serverless namespaces and
// workgroups. It deliberately avoids warehouse connections, query results,
// table contents, master user passwords, secret values, snapshot contents,
// IAM/resource policy JSON, and any mutation API. AWS SDK pagination and API
// telemetry live in the awssdk adapter.
package redshift
