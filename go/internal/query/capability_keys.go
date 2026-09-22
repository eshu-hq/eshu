// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6642: these six capability ids are named by root's own route handlers, not by the capability package. Moving this file into capability/ leaves all six undefined at root -- reproduce with the move plus `go build -gcflags=-e ./internal/query/`. Only CatalogKey, which both sides name, lives in capability/.

// The capability keys root's own handlers name. They used to be declared
// beside their registration in the contract_*.go files; those registrations
// moved to query/contract (#6642 Part C), and these keys stayed because the
// routes did. query/contract spells the same strings as literals rather than
// importing them, which is the form the capability sweep gate can resolve --
// see contract/capabilities_keys.go. A divergence between the two surfaces as
// a matrix row the YAML contract does not name, which
// TestCapabilityMatrixMatchesYAMLContract fails on.
const (
	graphSummaryPacketCapability             = "platform_impact.graph_summary_packet"
	relationshipsCatalogCapability           = "platform_impact.relationships_catalog"
	replatformingPlanReadinessCapability     = "replatforming.plan.readiness"
	replatformingSelectorInventoryCapability = "replatforming.selector_inventory"
	surfaceInventoryCapability               = "surface_inventory.list"
)
