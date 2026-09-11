// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package visualization

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// PacketDerivationCapability names the capability the derive route reports in
// its truth envelope. Root's contract_capability_matrix.go registers the same
// string for production; TestMain in this package registers it for the
// package's own tests, and the root lockstep test keeps the two in step.
const PacketDerivationCapability = "visualization.packet_derivation"

// PacketDerivationSupport returns the capability ceilings for
// PacketDerivationCapability: derived truth in every profile, because the
// route is a pure transform of a caller-supplied response that performs no
// graph or content read and embeds the source truth in the packet.
//
// The four ceilings get separate variables on purpose. Returning four
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other three.
func PacketDerivationSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelDerived
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
}
