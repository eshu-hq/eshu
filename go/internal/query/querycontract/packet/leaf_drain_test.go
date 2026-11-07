// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package packet

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestLeafBuildsRefusalPacket pins the leaf's core contract after the
// #9669 row-3 drain: the investigation evidence packet family moved here
// from root package packet, so eshu-cli builds and renders packets without
// importing the handler package. A refusal for an unknown family must stay
// a valid, share-safe artifact with no truth attached.
func TestLeafBuildsRefusalPacket(t *testing.T) {
	packet, err := NewInvestigationEvidencePacket(InvestigationPacketInput{
		Family:  InvestigationFamily("nope"),
		Subject: map[string]string{"scope": "test"},
	})
	if err != nil {
		t.Fatalf("refusal build failed: %v", err)
	}
	if packet.Refusal != PacketRefusalUnknownFamily {
		t.Fatalf("refusal = %q, want %q", packet.Refusal, PacketRefusalUnknownFamily)
	}
	if packet.Truth != nil {
		t.Fatal("refusal packet must carry no truth")
	}
	if packet.Schema != InvestigationEvidencePacketSchema {
		t.Fatalf("schema = %q, want %q", packet.Schema, InvestigationEvidencePacketSchema)
	}
	_ = querycontract.TruthEnvelope{Level: querycontract.TruthLevelExact}
}
