// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compparity

import (
	"testing"
)

// TestSupportedSupplyChainPacketIsSupportedComplete pins the fixture packet
// used by the investigation exercise: it must stay a supported, complete
// answer with all three evidence layers, or `competitive-parity validate`
// would silently prove nothing. Moved from
// go/cmd/eshu/competitive_parity_cmd_test.go with the packet builder.
func TestSupportedSupplyChainPacketIsSupportedComplete(t *testing.T) {
	packet, err := SupportedSupplyChainPacket()
	if err != nil {
		t.Fatalf("build supported packet: %v", err)
	}
	if !packet.Answer.Supported || packet.Answer.Partial {
		t.Fatalf("packet answer supported=%t partial=%t, want supported complete", packet.Answer.Supported, packet.Answer.Partial)
	}
	if len(packet.SourceFacts) == 0 || len(packet.ReducerDecisions) == 0 || len(packet.GraphAnswers) == 0 {
		t.Fatalf("packet missing supported evidence layers: source=%d reducer=%d graph=%d",
			len(packet.SourceFacts), len(packet.ReducerDecisions), len(packet.GraphAnswers))
	}
}
