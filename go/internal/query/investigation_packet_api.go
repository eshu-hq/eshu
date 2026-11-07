// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/evidence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

func packetBoundsFromRequest(r *http.Request) *packet.PacketBounds {
	maxSourceFacts := QueryParamInt(r, "max_source_facts", 0)
	if maxSourceFacts <= 0 {
		return nil
	}
	return &packet.PacketBounds{MaxSourceFacts: maxSourceFacts}
}

func writeInvestigationPacket(
	w http.ResponseWriter,
	r *http.Request,
	pkt packet.InvestigationEvidencePacket,
) {
	WriteSuccess(w, r, http.StatusOK, pkt, pkt.Truth)
}

func refusalPacketForAPI(
	family packet.InvestigationFamily,
	refusal packet.PacketRefusalState,
) (packet.InvestigationEvidencePacket, error) {
	return packet.NewInvestigationEvidencePacket(packet.InvestigationPacketInput{
		Family:  family,
		Subject: map[string]string{"scope": "unavailable"},
		Refusal: refusal,
	})
}

// === Lane-B packet responder (formerly chain_impact_packet_responder.go) ===

// Fixed bodies of the two packet build steps behind GET
// /api/v0/investigations/supply-chain/impact/packet (#7674). Neither builds
// from a read: a build error quotes packet contents, so it goes on the request
// span, never into the body.
const (
	supplyChainImpactPacketBuildFailedMessage   = "supply-chain impact packet build failed"
	supplyChainImpactRefusalPacketFailedMessage = "supply-chain impact refusal packet build failed"
)

// supplyChainImpactPacketResponder implements
// supplychain.ImpactPacketResponder from the lane-B packet
// envelope. It lives in root because the envelope types
// (packet.InvestigationEvidencePacket, packet.PacketBounds, the refusal
// composer) now live in the packet leaf; this responder stays in root so cmd
// wiring and the supply-chain hub keep their seam unchanged. the hub passes only leaf values and the live request, so bounds
// still come from packetBoundsFromRequest on the same request the route
// received — byte-identical to the pre-move route.
//
// The envelope moved to the packet leaf (#9669 row 3); the hub keeps
// calling through this responder so its seam and cmd wiring stay unchanged.
type supplyChainImpactPacketResponder struct{}

// NewSupplyChainImpactPacketResponder builds the lane-B packet responder
// cmd wiring injects into the supply-chain hub handler.
func NewSupplyChainImpactPacketResponder() supplychain.ImpactPacketResponder {
	return supplyChainImpactPacketResponder{}
}

// RespondSupplyChainImpactPacket composes body and truth into the portable
// packet and writes it, exactly as the pre-move getImpactPacket did.
func (supplyChainImpactPacketResponder) RespondSupplyChainImpactPacket(
	w http.ResponseWriter,
	r *http.Request,
	body packet.ExplanationResult,
	truth *querycontract.TruthEnvelope,
) {
	pkt, err := packet.BuildSupplyChainImpactPacket(body, truth, packetBoundsFromRequest(r))
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, supplyChainImpactPacketBuildFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, pkt)
}

// RespondSupplyChainImpactScopeRefusal writes the scope-not-found refusal
// packet, exactly as the pre-move getImpactPacket did.
func (supplyChainImpactPacketResponder) RespondSupplyChainImpactScopeRefusal(
	w http.ResponseWriter,
	r *http.Request,
) {
	pkt, err := refusalPacketForAPI(packet.InvestigationFamilySupplyChainImpact, packet.PacketRefusalScopeNotFound)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, supplyChainImpactRefusalPacketFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, pkt)
}

// Fixed bodies of the packet-build failure steps on GET
// /api/v0/investigations/deployable-unit/packet (#7674): the packet build
// and the scope-not-found refusal packet build. A build error quotes packet
// contents, so it goes on the request span, never into the body.
const (
	deployableUnitPacketBuildFailedMessage   = "deployable-unit packet build failed"
	deployableUnitRefusalPacketFailedMessage = "deployable-unit refusal packet build failed"
)

// deployableUnitPacketResponder implements
// evidence.DeployableUnitPacketResponder from the packet leaf envelope. It
// lives in root because cmd wiring and the evidence handler keep their seam; the evidence handler passes
// only leaf values and the live request, so bounds still come from
// packetBoundsFromRequest on the same request the route received —
// byte-identical to the pre-move route.
//
// The envelope moved to the packet leaf (#9669 row 3); the evidence handler
// keeps calling through this responder so its seam and cmd wiring stay unchanged.
type deployableUnitPacketResponder struct{}

// NewDeployableUnitPacketResponder builds the lane-B packet responder cmd
// wiring injects into the evidence handler.
func NewDeployableUnitPacketResponder() evidence.DeployableUnitPacketResponder {
	return deployableUnitPacketResponder{}
}

// RespondDeployableUnitPacket composes decisions and subject into the
// portable packet and writes it, exactly as the pre-move
// getDeployableUnitPacket did.
func (deployableUnitPacketResponder) RespondDeployableUnitPacket(
	w http.ResponseWriter,
	r *http.Request,
	decisions []packet.AdmissionDecisionResult,
	subject map[string]string,
	truth *querycontract.TruthEnvelope,
) {
	pkt, err := packet.BuildDeployableUnitPacket(decisions, subject, truth, packetBoundsFromRequest(r))
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, deployableUnitPacketBuildFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, pkt)
}

// RespondDeployableUnitScopeRefusal writes the scope-not-found refusal
// packet, exactly as the pre-move getDeployableUnitPacket did.
func (deployableUnitPacketResponder) RespondDeployableUnitScopeRefusal(
	w http.ResponseWriter,
	r *http.Request,
) {
	pkt, err := refusalPacketForAPI(packet.InvestigationFamilyDeployableUnit, packet.PacketRefusalScopeNotFound)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, deployableUnitRefusalPacketFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, pkt)
}
