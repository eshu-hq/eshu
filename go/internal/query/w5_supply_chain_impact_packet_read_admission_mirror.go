// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// supplyChainImpactPacketReadAdmissionProductionMirror proves the
// production stores jointly serve the #9669 W5 impact packet pin (GET
// /api/v0/investigations/supply-chain/impact/packet): *Neo4jReader and
// *ContentReader carry the shared selector closure (graph fallback
// plus MatchRepositories), PostgresFindingStore carries the
// explanation read, and PostgresReadinessStore carries the readiness
// read. The assignment below fails `go build`, not only `go test`, the
// moment a pin method is removed, renamed, or re-signed on any
// production store. It works in one direction only: a new method on any
// production store does not widen the pin — adoption takes deliberate
// re-declaration in chain plus a frozen-list update. It pins no shared
// port — this assertion names the production stores only as the shapes
// that jointly serve the pin, it keeps the handler's separate store
// fields unchanged, it does not widen the D6 pin, and no other route
// may cite this pin as coverage.
//
// Rollback: delete this file alongside the pin. It asserts only; it
// wires no handler, route, or caller.
type supplyChainImpactPacketReadAdmissionProductionMirror struct {
	*Neo4jReader
	*ContentReader
	impact.PostgresFindingStore
	impact.PostgresReadinessStore
}

var _ supplychain.SupplyChainImpactPacketReadAdmission = (*supplyChainImpactPacketReadAdmissionProductionMirror)(nil)
