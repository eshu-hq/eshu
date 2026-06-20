package query

import (
	"net/http"
)

func packetBoundsFromRequest(r *http.Request) *PacketBounds {
	maxSourceFacts := QueryParamInt(r, "max_source_facts", 0)
	if maxSourceFacts <= 0 {
		return nil
	}
	return &PacketBounds{MaxSourceFacts: maxSourceFacts}
}

func writeInvestigationPacket(
	w http.ResponseWriter,
	r *http.Request,
	packet InvestigationEvidencePacket,
) {
	WriteSuccess(w, r, http.StatusOK, packet, packet.Truth)
}

func refusalPacketForAPI(
	family InvestigationFamily,
	refusal PacketRefusalState,
) (InvestigationEvidencePacket, error) {
	return NewInvestigationEvidencePacket(InvestigationPacketInput{
		Family:  family,
		Subject: map[string]string{"scope": "unavailable"},
		Refusal: refusal,
	})
}
