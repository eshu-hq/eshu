// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudasset resolves canonical cloud asset identity across sources
// and persists one durable reducer_cloud_asset_resolution fact per
// reconciliation intent.
//
// [CloudAssetResolutionHandler] normalizes an intent's entity keys and
// related scope ids (deduplicated and sorted for stable identity), rejects an
// intent with no entity keys, dispatches the write through
// [CloudAssetResolutionWriter], and publishes the canonical-nodes-committed
// graph phase for the cloud_resource_uid keyspace on success.
// [PostgresCloudAssetResolutionWriter] is the production adapter, storing the
// reconciliation as a single-row canonical reducer fact.
//
// The domain constant (contract.DomainCloudAssetResolution) and its
// DomainDefinition stay in the reducer root's core catalog
// (DefaultDomainDefinitions); this package supplies only the handler and
// writer, wired in implementedDefaultDomainDefinitions.
//
// reducer_cloud_asset_resolution currently has no production query, MCP, or
// storage consumer (documented exemption in
// docs/internal/design/4784-reducer-derived-fact-governance.md); the public
// cloud inventory read contract lives on reducer_cloud_resource_identity
// instead.
//
// This package never imports internal/reducer.
package cloudasset
