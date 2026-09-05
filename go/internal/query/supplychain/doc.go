// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychain implements the supply-chain query hub: the
// SupplyChainHandler HTTP surface and the read-model ports it serves.
//
// The handler exposes nineteen routes over reducer-owned supply-chain truth:
// the vulnerability-scanner read contract, SBOM attestation attachments
// (list plus aggregate count/inventory), the advisory catalog and advisory
// evidence (backed by the advisory subpackage), impact findings, impact
// explanation, impact aggregates, and the impact investigation packet
// (backed by the impact subpackage), container image identities (list plus
// aggregates), security alert reconciliations (list plus aggregates), the
// vulnerability-suppression mutation, and the vulnerability detail read.
//
// It moved out of root package query (#6060 lane A) so the family can be
// read, tested, and changed without pulling in the rest of the query
// surface. It depends only on dependency-neutral leaves -- querycontract
// (envelopes, capabilities, row-value decoders), queryauth (request auth
// bounds), queryselector (repository-selector resolution), queryspan (span
// plumbing), querydecode-adjacent sdk/go/factschema seams, internal/scope,
// and the advisory/impact subpackages -- never on root package query
// itself, which would create an import cycle: root's
// supply_chain_hub_alias.go imports this package for the compatibility
// aliases cmd/api and cmd/mcp-server still use.
//
// Three contracts callers must hold. Every list route requires a bounded
// scope or an explicit limit: an unscoped read over a whole fact corpus is
// rejected before any store runs. The runtime-evidence probes (cloud,
// Kubernetes, runtime context) only promote a finding to runtime_confirmed
// on current, caller-authorized evidence; a nil inventory store disables
// its probe tier rather than surfacing unauthorized or stale evidence. The
// impact packet route composes through the injected
// SupplyChainImpactPacketResponder, which root provides from the lane-B
// packet envelope: the hub never imports the packet types directly.
//
// Capability registration stays in root package query
// (contract_supply_chain.go), which owns the router and always links into
// the production binary. The Postgres store implementations stay in root
// too (shared with entity and incident-context reads); this package owns
// the store ports, the row/filter/page values crossing them, and the
// handler. See README.md for the full boundary and AGENTS.md for the
// per-symbol export list.
package supplychain
