// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact implements the supply-chain impact query read models: the
// reducer-owned vulnerability impact findings (PostgresSupplyChainImpactFindingStore,
// bounded list/explain reads over active impact-finding facts), the impact
// aggregates (PostgresSupplyChainImpactAggregateStore, counts and inventory
// rollups), the evidence-readiness snapshots
// (PostgresSupplyChainImpactReadinessStore, per-family counts and freshness),
// the read-time runtime-context and runtime-environment evidence joins, the
// scanner-filter and suppression-mutation Postgres adapters, and the
// remediation, path, priority, version-resolution, and source-state read
// models the findings handlers assemble into responses.
//
// It moved out of root package query (#6060 lane A) so this family can be
// read, tested, and changed without pulling in the rest of the query surface.
// It depends only on dependency-neutral leaves -- querycontract (row-value
// decoders, HTTP param/error writers), querydecode (classified fact-decode
// failures), sdk/go/factschema (the typed source-fact decode seams),
// pgarray, postgres (the suppression-mutation storage adapter), and truth --
// never on root package query itself, which would create an import cycle:
// root's supply_chain_impact_alias.go imports this package for the
// compatibility aliases cmd/api, cmd/mcp-server, internal/serviceintelhttp,
// and internal/cli still use.
//
// Three contracts callers must hold. A findings-list caller must scope the
// filter (HasScope): an unscoped read over the whole impact-finding corpus is
// rejected before any SQL runs, and the page limit is bounded by
// supplyChainImpactFindingMaxLimit. A readiness caller must anchor the query
// (CVE, package, repository, subject digest, or image reference); derived
// scanner filters alone do not open a source-fact scan. A caller reading a
// decoded row must treat a dropped contribution as a dead-lettered malformed
// fact (input_invalid), not as missing data: facts that fail typed-decode
// validation never contribute zero-valued rows; the raw-payload path stays
// only for reducer-derived kinds with no typed struct yet (see the #4784 ADR
// note in supply_chain_impact_decode_helpers.go).
//
// Capability registration stays in root package query
// (contract_supply_chain.go), which owns the router and always links into
// the production binary. The handler, probe, and scope files stay in root
// until the hub PR3, so most impact tests stay there too and reach this
// package as impact.X; the pure read-model unit tests moved here (see
// README.md for which tests live where and why).
package impact
