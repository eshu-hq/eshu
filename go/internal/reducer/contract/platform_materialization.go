// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// PlatformMaterializationFactKind names the durable fact kind the
// deployment_mapping (platform materialization) writer publishes under. It is
// exported so the platformfam family can write it and the reducer root's
// supply-chain-impact index can read it without either importing the other,
// keeping the package-import direction strictly downward
// (root -> family -> shared-core -> contract).
const PlatformMaterializationFactKind = "reducer_platform_materialization"
