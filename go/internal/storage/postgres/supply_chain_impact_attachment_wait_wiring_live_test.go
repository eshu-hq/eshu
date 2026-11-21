// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// supplyChainImpactAttachmentNotReadyClass is the #11105 failure class the
// impact pass defers with while its own attachment is in flight.
const supplyChainImpactAttachmentNotReadyClass = "supply_chain_impact_sbom_attachment_not_ready"

// wireAttachmentReadiness wires the #11105 attachment readiness port the way
// cmd/reducer does. Before the fix there is no port to wire.
func wireAttachmentReadiness(_ *supplychaincore.SupplyChainImpactHandler, _ *sql.DB) {}
