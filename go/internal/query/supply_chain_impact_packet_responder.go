// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// supplyChainImpactPacketResponder implements
// supplychain.SupplyChainImpactPacketResponder from the lane-B packet
// envelope. It lives in root because the envelope types
// (InvestigationEvidencePacket, PacketBounds, the refusal composer) live
// here; the hub passes only leaf values and the live request, so bounds
// still come from packetBoundsFromRequest on the same request the route
// received — byte-identical to the pre-move route.
//
// If lane-B moves the envelope to an importable leaf, delete this type and
// have the hub call the leaf directly.
type supplyChainImpactPacketResponder struct{}

// NewSupplyChainImpactPacketResponder builds the lane-B packet responder
// cmd wiring injects into the supply-chain hub handler.
func NewSupplyChainImpactPacketResponder() supplychain.SupplyChainImpactPacketResponder {
	return supplyChainImpactPacketResponder{}
}

// RespondSupplyChainImpactPacket composes body and truth into the portable
// packet and writes it, exactly as the pre-move getImpactPacket did.
func (supplyChainImpactPacketResponder) RespondSupplyChainImpactPacket(
	w http.ResponseWriter,
	r *http.Request,
	body impact.SupplyChainImpactExplanationResult,
	truth *querycontract.TruthEnvelope,
) {
	packet, err := BuildSupplyChainImpactPacket(body, truth, packetBoundsFromRequest(r))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeInvestigationPacket(w, r, packet)
}

// RespondSupplyChainImpactScopeRefusal writes the scope-not-found refusal
// packet, exactly as the pre-move getImpactPacket did.
func (supplyChainImpactPacketResponder) RespondSupplyChainImpactScopeRefusal(
	w http.ResponseWriter,
	r *http.Request,
) {
	packet, err := refusalPacketForAPI(InvestigationFamilySupplyChainImpact, PacketRefusalScopeNotFound)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeInvestigationPacket(w, r, packet)
}
